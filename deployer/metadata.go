package deployer

import (
	"github.com/dorzheh/deployer/utils/hwinfo/guest"
)

// MetadataConfigurator is the interface that has to be implemented
// in order to manipulate appropriate metadata.
type MetadataConfigurator interface {
	// CPU configuration
	// Returns metadata entry related to the guest CPU configuration
	SetCpuConfigData(*guest.Config, string) (string, error)

	// vCPU and list of physical CPUs the vCPU is bound to and templates directory.
	// Returns vCPU related metadata entry and error.
	SetCpuTuneData(*guest.Config, string) (string, error)

	// NUMA configuration
	// Returns NUMA tuning related metadata entry and error.
	SetNUMATuneData(*guest.Config, string) (string, error)

	// Storage configuration and templates directory.
	// Returns storage related metadata entry and error.
	SetStorageData(*guest.Config, string) (string, error)

	// Network interfaces information, templates directory.
	// Returns metadata entry related to the network interfaces configuration and error.
	SetNetworkData(*guest.Config, string) (string, error)

	// Allows to implement a custom logic related to a metadata configuration.
	// Returns a metadata entry and error.
	SetCustomData(*guest.Config, string) (string, error)

	// Default metadata is used by deployer in case user didn't provide any template.
	// Returns entry related to default metadata.
	DefaultMetadata() []byte
}
