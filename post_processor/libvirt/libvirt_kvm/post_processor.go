package libvirt_kvm

import (
	"regexp"

	"github.com/dorzheh/deployer/deployer"
	"github.com/dorzheh/deployer/drivers/env_driver/libvirt/libvirt_kvm"
	"github.com/dorzheh/deployer/utils"
	ssh "github.com/dorzheh/infra/comm/common"
)

type PostProcessor struct {
	driver      *libvirt_kvm.Driver
	startDomain bool
}

func NewPostProcessor(sshconf *ssh.Config, startDomain bool) *PostProcessor {
	p := new(PostProcessor)
	p.driver = libvirt_kvm.NewDriver(sshconf)
	p.startDomain = startDomain
	return p
}

func (p *PostProcessor) PostProcess(artifacts []deployer.Artifact) error {
	for _, a := range artifacts {
		switch a.(type) {
		case *deployer.CommonArtifact:
			if a.GetType() == deployer.MetadataArtifact {
				if err := p.driver.DefineDomain(a.GetPath()); err != nil {
					return utils.FormatError(err)
				}

				out, err := p.driver.Run("cat " + a.GetPath())
				if err != nil {
					return utils.FormatError(err)
				}

				r, err := regexp.Compile(`<name>\s*(\S+)\s*</name>`)
				if err != nil {
					return utils.FormatError(err)
				}

				domain := r.FindStringSubmatch(out)[1]
				if err := p.driver.SetAutostart(domain); err != nil {
					return utils.FormatError(err)
				}
				if p.startDomain {
					if err := p.driver.StartDomain(domain); err != nil {
						return utils.FormatError(err)
					}
				}
				if err := a.Destroy(); err != nil {
					return utils.FormatError(err)
				}
			}
		}
	}
	return nil
}
