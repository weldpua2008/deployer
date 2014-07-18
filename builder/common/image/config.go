// Parses image configuration file (XML)

// Configuration example:
//
//<?xml version="1.0" encoding="UTF-8"?>
//<Platforms>
//  <Topology>
//    <Name>Test</Name>
//    <Type>1</Type>
//	<HddSizeGb>5</HddSizeGb>
//  <Bootable>true</Bootable>
//	<FdiskCmd>n\np\n1\n\n+3045M\nn\np\n2\n\n\nt\n2\n82\na\n1\nw\n</FdiskCmd>
//    <Description>Topology for release xxxx</Description>
//    <Partition>
//	  <Sequence>1</Sequence>
//	  <SizeMb>3045</SizeMb>
//      <Label>SLASH</Label>
//      <MountPoint>/</MountPoint>
//      <FileSystem>ext4</FileSystem>
//	  <FileSystemArgs></FileSystemArgs>
//	</Partition>
//	<Partition>
//	  <Sequence>2</Sequence>
//	  <SizeMb>400</SizeMb>
//      <Label>SWAP</Label>
//      <MountPoint>SWAP</MountPoint>
//      <FileSystem>swap</FileSystem>
//	  <FileSystemArgs></FileSystemArgs>
//	</Partition>
//  </Topology>
//</Platforms>`

package image

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io/ioutil"
)

type TopologyType uint8

type Platforms struct {
	Platforms []Topology `xml:"Topology"`
}

type Topology struct {
	Name        string      `xml:"Name"`
	Type        string      `xml:"Type"`
	HddSizeGb   int         `xml:"HddSizeGb"`
	Bootable    bool        `xml:"Bootable"`
	FdiskCmd    string      `xml: "FdiskCmd"`
	Description string      `xml:"Description"`
	Partitions  []Partition `xml:"Partition"`
}

type Partition struct {
	Sequence       string `xml:"Sequence"`
	SizeMb         int    `xml:"SizeMb"`
	Label          string `xml:"Label"`
	MountPoint     string `xml:"MountPoint"`
	FileSystem     string `xml:"FileSystem"`
	FileSystemArgs string `xml:"FileSystemArgs"`
	Description    string `xml:"description"`
}

// ParseConfigFile is responsible for reading appropriate XML file
// and calling ParseConfig for further processing
func ParseConfigFile(xmlpath string) (*Platforms, error) {
	fb, err := ioutil.ReadFile(xmlpath)
	if err != nil {
		return nil, err
	}
	return ParseConfig(fb)
}

// ParseConfig is responsible for processing XML content
func ParseConfig(fb []byte) (*Platforms, error) {
	buf := bytes.NewBuffer(fb)
	p := new(Platforms)
	decoded := xml.NewDecoder(buf)
	if err := decoded.Decode(p); err != nil {
		return nil, err
	}
	return p, nil
}

// TypeToTopology returns a topology configuration related to a type
func (p *Platforms) TypeToTopology(topotype TopologyType) (*Topology, error) {
	t := &p.Platforms[topotype]
	if t == nil {
		return nil, fmt.Errorf("no topology found for type %d", topotype)
	}
	return t, nil
}
