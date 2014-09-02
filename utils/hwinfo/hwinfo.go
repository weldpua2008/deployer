// Responsible for parsing lshw output

package hwinfo

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/dorzheh/deployer/utils"
	ssh "github.com/dorzheh/infra/comm/common"
	"github.com/dorzheh/infra/utils/lshw"
	"github.com/dorzheh/mxj"
)

type Parser struct {
	run       func(string) (string, error)
	cacheFile string
	cmd       string
}

// NewParser constructs new lshw parser
// The output will be represented in JSON format
func NewParser(cacheFile, lshwpath string, sshconf *ssh.Config) (*Parser, error) {
	i := new(Parser)
	i.run = utils.RunFunc(sshconf)
	if lshwpath == "" {
		out, err := i.run("which lshw")
		if err != nil {
			return nil, fmt.Errorf("%s [%v]", out, err)
		}
		lshwpath = out
	} else {
		if sshconf != nil {
			dir, err := utils.UploadBinaries(sshconf, lshwpath)
			if err != nil {
				return nil, err
			}
			lshwpath = filepath.Join(dir, filepath.Base(lshwpath))
		}
	}

	lshwconf := &lshw.Config{[]lshw.Class{lshw.All}, lshw.FormatJSON}
	l, err := lshw.New(lshwpath, lshwconf)
	if err != nil {
		return nil, err
	}
	i.cmd = l.Cmd()
	i.cacheFile = cacheFile
	return i, nil
}

// Parse parses lshw output
func (i *Parser) Parse() error {
	out, err := i.run(i.cmd)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(i.cacheFile, []byte(out), 0)
}

// CPU contains CPU description and properties
type CPU struct {
	Desc map[string]interface{}
	Cap  map[string]interface{}
}

// CPUInfo gathers information related to installed CPUs
func (i *Parser) CPUInfo() (*CPU, error) {
	if _, err := os.Stat(i.cacheFile); err != nil {
		if err = i.Parse(); err != nil {
			return nil, err
		}
	}

	out, err := mxj.NewMapsFromJsonFile(i.cacheFile)
	if err != nil {
		return nil, err
	}

	c := new(CPU)
	c.Desc = make(map[string]interface{})
	c.Cap = make(map[string]interface{})
	for _, s := range out {
		r, _ := s.ValuesForPath("children.children")
		for _, n := range r {
			ch := n.(map[string]interface{})
			if ch["id"] == "cpu:0" {
				for k, v := range ch {
					if k != "capabilities" {
						c.Desc[k] = v
					}
				}
				for k, v := range ch["capabilities"].(map[string]interface{}) {
					c.Cap[k] = v
				}
			}
		}
	}
	return c, nil
}

func (p *Parser) CPUs() (uint, error) {
	cpustr, err := p.run(`grep -c ^processor /proc/cpuinfo`)
	if err != nil {
		return 0, err
	}

	cpus, err := strconv.Atoi(strings.Trim(cpustr, "\n"))
	if err != nil {
		return 0, err
	}
	return uint(cpus), nil
}

// supported NIC types
type NicType string

const (
	NicTypePhys   NicType = "physical"
	NicTypeOVS    NicType = "openvswitch"
	NicTypeBridge NicType = "bridge"
)

// NIC information
type NIC struct {
	// port name (eth0,br0...)
	Name string

	// NIC driver(bridge,openvswitch...)
	Driver string

	// Description
	Desc string

	// PCI Address
	PCIAddr string

	// Port type
	Type NicType
}

// NICInfo gathers information related to installed NICs
func (p *Parser) NICInfo() ([]*NIC, error) {
	if _, err := os.Stat(p.cacheFile); err != nil {
		if err = p.Parse(); err != nil {
			return nil, err
		}
	}
	out, err := mxj.NewMapsFromJsonFile(p.cacheFile)
	if err != nil {
		return nil, err
	}

	nics := make([]*NIC, 0)
	deep := []string{"children.children.children.children.children",
		"children.children.children.children",
		"children.children.children",
		"children.children",
		"children"}
	for _, m := range out {
		for _, d := range deep {
			r, _ := m.ValuesForPath(d)
			for _, n := range r {
				ch := n.(map[string]interface{})
				if ch["description"] == "Ethernet interface" {
					name := ch["logicalname"].(string)
					if name == "ovs-system" {
						continue
					}
					nic := new(NIC)
					nic.Name = name
					driver := ch["configuration"].(map[string]interface{})["driver"].(string)
					switch driver {
					case "tun":
						continue
					case "openvswitch":
						nic.Desc = "Open vSwitch interface"
						nic.Type = NicTypeOVS
					default:
						prod, ok := ch["product"].(string)
						if ok {
							vendor, _ := ch["vendor"].(string)
							logicalname := ch["logicalname"].(string)
							if _, err := p.run(fmt.Sprintf("[[ -d /sys/class/net/%s/master || -d /sys/class/net/%s/brport ]]", logicalname, logicalname)); err == nil {
								continue
							}
							nic.PCIAddr = ch["businfo"].(string)
							nic.Desc = vendor + " " + prod
							nic.Type = NicTypePhys
						}
					}
					nic.Driver = driver
					nics = append(nics, nic)
				}
			}
		}
	}

	// lshw is unable to find linux bridges so let's do it manually
	res, err := p.run(`out="";for n in /sys/class/net/*;do [ -d $n/bridge ] && out="$out ${n##/sys/class/net/}";done;echo $out`)
	if err != nil {
		return nil, err
	}
	if res != "" {
		for _, n := range strings.Split(res, " ") {
			br := &NIC{
				Name:   n,
				Driver: "bridge",
				Desc:   "Bridge interface",
				Type:   NicTypeBridge,
			}
			nics = append(nics, br)
		}
	}
	sort.Sort(sortByPCI(nics))
	return nics, nil
}

func (p *Parser) NUMANodes() (map[uint][]uint, error) {
	out, err := p.run("ls -d  /sys/devices/system/node/node[0-9]*")
	if err != nil {
		return nil, err
	}

	numaMap := make(map[uint][]uint)
	for i, _ := range strings.SplitAfter(out, "\n") {
		out, err := p.run(fmt.Sprintf("ls -d  /sys/devices/system/node/node%d/cpu[0-9]*", i))
		if err != nil {
			return nil, err
		}
		cpus := make([]uint, 0)
		for _, line := range strings.SplitAfter(out, "\n") {
			cpuStr := strings.SplitAfter(line, "cpu")[1]
			cpu, err := strconv.Atoi(strings.TrimSpace(cpuStr))
			if err != nil {
				return nil, err
			}
			cpus = append(cpus, uint(cpu))
		}
		numaMap[uint(i)] = cpus
	}
	return numaMap, nil
}

// RAMSize gathers information related to the installed amount of RAM in MB
func (p *Parser) RAMSize() (uint, error) {
	out, err := p.run("grep MemTotal /proc/meminfo")
	if err != nil {
		return 0, err
	}
	var col3 string
	var ramsize uint
	fmt.Sscanf(out, "MemTotal: %d %s", &ramsize, &col3)
	return ramsize / 1024, nil
}

type sortByPCI []*NIC

func (s sortByPCI) Len() int           { return len(s) }
func (s sortByPCI) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s sortByPCI) Less(i, j int) bool { return s[i].PCIAddr < s[j].PCIAddr }