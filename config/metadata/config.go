package metadata

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/dorzheh/deployer/builder/image"
	"github.com/dorzheh/deployer/config"
	"github.com/dorzheh/deployer/config/bundle"
	"github.com/dorzheh/deployer/config/common"
	"github.com/dorzheh/deployer/config/xmlinput"
	"github.com/dorzheh/deployer/controller"
	"github.com/dorzheh/deployer/deployer"
	gui "github.com/dorzheh/deployer/ui"
	"github.com/dorzheh/deployer/utils"
	"github.com/dorzheh/deployer/utils/host_hwfilter"
	"github.com/dorzheh/deployer/utils/hwinfo/guest"
)

// InputData provides a static data
type InputData struct {
	// Path to the lshw binary
	Lshw string

	// Path to the basic configuration file (see xmlinput package)
	InputDataConfigFile string

	// Path to the bundle configuration file (see bundle package)
	BundleDataConfigFile string

	// Path to the storage configuration file (see image package)
	StorageConfigFile string

	// Bundle parser
	BundleParser *bundle.Parser

	// Path to directory containing appropriate templates intended
	// for overriding default configuration
	TemplatesDir string
}

// Metadata contains elements that will processed
// by the template library and used by Libvirt XML metadata
type Metadata struct {
	DomainName   string
	RAM          int
	CPUs         int
	CPUTune      string
	CPUConfig    string
	NUMATune     string
	EmulatorPath string
	Storage      string
	Networks     string
	CustomData   interface{}
}

// Config contains common configuration plus appropriate
// configuration required for appliances powered by environment
// based on libvirt API
type Config struct {
	// Common configuration
	*deployer.CommonConfig

	// Hostinfo driver
	Hwdriver deployer.HostinfoDriver

	// Environment driver
	EnvDriver deployer.EnvDriver

	GuestConfig *guest.Config

	// Common metadata stuff
	Metadata *Metadata

	// Path to metadata file
	DestMetadataFile string

	// Bundle config
	Bundle map[string]interface{}
}

func NewMetdataConfig(d *deployer.CommonData, storageConfigFile string) (*Config, error) {
	common, err := common.RegisterSteps(d, storageConfigFile)
	if err != nil {
		return nil, utils.FormatError(err)
	}
	mcfg := &Config{common, nil, nil, nil, nil, "", nil}
	mcfg.GuestConfig = guest.NewConfig()
	return mcfg, nil
}

func RegisterSteps(d *deployer.CommonData, i *InputData, c *Config, metaconf deployer.MetadataConfigurator) error {
	xid, err := xmlinput.ParseXMLInput(i.InputDataConfigFile)
	if err != nil {
		return utils.FormatError(err)
	}

	controller.RegisterSteps(func() func() error {
		return func() error {
			var err error
			if c.Metadata.DomainName, err = gui.UiApplianceName(d.Ui, d.VaName, c.EnvDriver); err != nil {
				return err
			}
			d.VaName = c.Metadata.DomainName
			if err = gui.UiGatherHWInfo(d.Ui, c.Hwdriver, c.RemoteMode); err != nil {
				return utils.FormatError(err)
			}
			return nil
		}
	}())

	// Network configuration
	if xid.Networks.Configure {
		controller.RegisterSteps(func() func() error {
			return func() error {
				c.GuestConfig.Networks = nil
				c.GuestConfig.NICLists = nil

				nics, err := host_hwfilter.GetAllowedNICs(xid, c.Hwdriver)
				if err != nil {
					return utils.FormatError(err)
				}
				if err = gui.UiNetworks(d.Ui, xid, nics, c.GuestConfig); err != nil {
					return err
				}
				c.Metadata.Networks, err = metaconf.SetNetworkData(c.GuestConfig, i.TemplatesDir)
				if err != nil {
					return utils.FormatError(err)
				}
				return nil
			}

		}())
	}

	// guest configuration
	controller.RegisterSteps(func() func() error {
		return func() error {
			if i.BundleParser != nil {
				m, err := i.BundleParser.Parse(d, c.Hwdriver, xid)
				if err != nil {
					return utils.FormatError(err)
				}
				if m != nil {
					c.Bundle = m
					c.GuestConfig.CPUs = m["cpus"].(int)
					c.GuestConfig.RamMb = m["ram_mb"].(int) * 1024
					c.GuestConfig.Storage, err = config.StorageConfig(filepath.Join(c.ExportDir, d.VaName),
						m["storage_config_index"].(image.ConfigIndex), c.StorageConfig, nil)
					if err != nil {
						return utils.FormatError(err)
					}
				}
			}
			if len(c.Bundle) == 0 {
				if xid.CPU.Max == xmlinput.UnlimitedAlloc {
					xid.CPU.Max = c.EnvDriver.MaxVCPUsPerGuest()
				}
				if err = gui.UiVmConfig(d.Ui, c.Hwdriver, xid, filepath.Join(c.ExportDir, d.VaName), c.StorageConfig, c.GuestConfig); err != nil {
					return err
				}
			}

			c.Metadata.CPUs = c.GuestConfig.CPUs
			c.Metadata.RAM = c.GuestConfig.RamMb * 1024
			if c.GuestConfig.Storage == nil {
				if c.GuestConfig.Storage, err = config.StorageConfig(filepath.Join(c.ExportDir, d.VaName), 0, c.StorageConfig, nil); err != nil {
					return err
				}
			}
			c.Metadata.Storage, err = metaconf.SetStorageData(c.GuestConfig, i.TemplatesDir)
			if err != nil {
				return utils.FormatError(err)
			}
			return nil

		}
	}())

	// NUMA configuration
	controller.RegisterSteps(func() func() error {
		return func() error {
			c.GuestConfig.NUMAs = nil
			numas, err := c.Hwdriver.NUMAInfo()
			if err != nil {
				return utils.FormatError(err)
			}
			if xid.NUMA.AdvancedAutoConfig {
				if numas.TotalNUMAs() == 1 {
					if err := c.GuestConfig.SetTopologySingleVirtualNUMA(numas, true); err != nil {
						return utils.FormatError(err)
					}
				} else {
					if err := c.GuestConfig.SetTopologyMultipleVirtualNUMAs(numas); err != nil {
						return utils.FormatError(err)
					}
				}
			} else {
				if err := c.GuestConfig.SetTopologySingleVirtualNUMA(numas, false); err != nil {
					return utils.FormatError(err)
				}
			}

			cpus, err := c.Hwdriver.CPUs()
			if err != nil {
				return err
			}
			if err := gui.UiNUMATopology(d.Ui, c.GuestConfig, cpus); err != nil {
				return err
			}

			c.Metadata.CPUTune, err = metaconf.SetCpuTuneData(c.GuestConfig, i.TemplatesDir)
			if err != nil {
				return utils.FormatError(err)
			}
			c.Metadata.CPUConfig, err = metaconf.SetCpuConfigData(c.GuestConfig, i.TemplatesDir)
			if err != nil {
				return utils.FormatError(err)
			}
			c.Metadata.NUMATune, err = metaconf.SetNUMATuneData(c.GuestConfig, i.TemplatesDir)
			if err != nil {
				return utils.FormatError(err)
			}
			return nil
		}
	}())

	// create default metadata
	controller.RegisterSteps(func() func() error {
		return func() error {
			c.DestMetadataFile = fmt.Sprintf("/tmp/%s-temp-metadata.%d", d.VaName, os.Getpid())
			// always create default metadata
			if err := ioutil.WriteFile(c.DestMetadataFile, metaconf.DefaultMetadata(), 0); err != nil {
				return utils.FormatError(err)
			}
			return controller.SkipStep
		}
	}())

	return nil
}

func ProcessNetworkTemplate(mode *xmlinput.Mode, defaultTemplate string, tmpltData interface{}, templatesDir string) (string, error) {
	var customTemplate string

	if mode.Tmplt == nil {
		customTemplate = defaultTemplate
	} else {
		var templatePath string
		if templatesDir != "" {
			templatePath = filepath.Join(templatesDir, mode.Tmplt.FileName)
		} else {
			templatePath = filepath.Join(mode.Tmplt.Dir, mode.Tmplt.FileName)
		}

		buf, err := ioutil.ReadFile(templatePath)
		if err != nil {
			return "", utils.FormatError(err)
		}
		customTemplate = string(buf)
	}

	tempData, err := utils.ProcessTemplate(customTemplate, tmpltData)
	if err != nil {
		return "", utils.FormatError(err)
	}
	return string(tempData) + "\n", nil
}
