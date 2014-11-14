package bundle

import (
	"errors"

	"github.com/dorzheh/deployer/config/common/xmlinput"
	"github.com/dorzheh/deployer/deployer"
	"github.com/dorzheh/deployer/ui"
)

func GetConfig(d *deployer.CommonData, hidriver deployer.HostinfoDriver,
	xid *xmlinput.XMLInputData, bundleConfigFile string) (*Config, error) {

	b, err := ParseConfigFile(bundleConfigFile)
	if err != nil {
		return nil, err
	}

	hostRamsizeMb, err := hidriver.RAMSize()
	if err != nil {
		return nil, err
	}

	configs := b.GetConfigs(hostRamsizeMb)
	amountOfConfigs := len(configs)
	if amountOfConfigs == 0 {
		return nil, errors.New("no eligable configuration is available for the host")
	}

	installedCpus, err := hidriver.CPUs()
	if err != nil {
		return nil, err
	}

	config := new(Config)
	for {
		c, err := uiBundleConfig(d.Ui, configs)
		if err != nil {
			return nil, err
		}
		if c == nil {
			break
		}
		if c.CPUs > installedCpus {
			if !ui.UiVCPUsOvercommit(d.Ui, installedCpus) {
				continue
			}
		}
		return c, nil
	}

	config.RAM = ui.UiRAMSize(d.Ui, hostRamsizeMb, xid.RAM.Min, xid.RAM.Max)
	config.CPUs = ui.UiCPUs(d.Ui, installedCpus, xid.CPU.Min, xid.CPU.Max)
	return config, nil
}