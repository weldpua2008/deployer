// Intended for creating a common configuration
// required by either target
package common

import (
	"fmt"

	"github.com/dorzheh/deployer/builder/common/image"
	"github.com/dorzheh/deployer/deployer"
	gui "github.com/dorzheh/deployer/ui"
)

func CreateConfig(d *deployer.CommonData) *deployer.CommonConfig {
	c := new(deployer.CommonConfig)
	c.RemoteMode = gui.UiRemoteMode(d.Ui)
	if c.RemoteMode {
		c.SshConfig = gui.UiSshConfig(d.Ui)
	}
	c.ExportDir = gui.UiImagePath(d.Ui, d.DefaultExportDir, c.RemoteMode)
	return c
}

func StorageConfig(storageConfigFile, pathToMainImage string, configIndex image.ConfigIndex) (*image.Config, error) {
	f, err := image.ParseConfigFile(storageConfigFile)
	if err != nil {
		return nil, err
	}

	conf, err := f.IndexToConfig(configIndex)
	if err != nil {
		return nil, err
	}

	var deviceIndex uint
	amountOfDisks := len(conf.Disks)
	for ; amountOfDisks != 0; amountOfDisks-- {
		if deviceIndex == 0 {
			conf.Disks[deviceIndex].Path = fmt.Sprintf("%s.%s", pathToMainImage, conf.Disks[deviceIndex].Type)
		} else {
			conf.Disks[deviceIndex].Path = fmt.Sprintf("%s_%d.%s", pathToMainImage, deviceIndex, conf.Disks[deviceIndex].Type)
		}
		deviceIndex++
	}
	return conf, nil
}
