// Responsible for creating , partitioning and customizing RAW image and writing MBR

package image

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/dorzheh/deployer/builder/content"
	"github.com/dorzheh/deployer/utils"
	"github.com/dorzheh/infra/comm/sshfs"
)

// the structure represents a mapper device
type mapperDevice struct {
	// device name(full path)
	name string

	// mount point the device is mounted
	mountPoint string
}

type loopDevice struct {
	// device name(full path)
	name string

	// slice of mapperDevice pointers
	mappers []*mapperDevice

	// counter - amount of mappers the loop device contains
	amountOfMappers uint8
}

// the structure represents loop device manipulation
type image struct {
	// storage configuration file
	config *Disk

	// path to the root directory where the image will be mounted
	slashpath string

	// do we need to partition and format the image
	needToFormat bool

	// loop device structure
	*loopDevice

	// set of utilities needed for image manipulation
	utils *Utils

	// path to sshfs mount
	// due to the fact that it used only during remote deployment mode
	// this indicates whether image creation occurs locally or remotely
	localmount string

	// executes commands locally or remotely
	run func(string) (string, error)

	// sshfs client
	client *sshfs.Client
}

type Utils struct {
	Kpartx string
	dir    string
}

//// Public methods ////

// New gets a path to configuration directory
// path to temporary directory where the vHDD image supposed to be mounted
// and path to vHDD image.
// Returns a pointer to the structure and error/nil
func New(config *Disk, rootfsMp string, bins *Utils, remoteConfig *sshfs.Config) (i *image, err error) {
	i = new(image)
	i.needToFormat = false
	var qemuImgError string

	if remoteConfig == nil {
		i.run = utils.RunFunc(nil)
		i.slashpath = rootfsMp
		i.utils = bins
		qemuImgError = "please install qemu-img"
	} else {
		i.run = utils.RunFunc(remoteConfig.Common)
		i.client, err = sshfs.NewClient(remoteConfig)
		if err != nil {
			err = utils.FormatError(err)
			return
		}
		i.localmount = rootfsMp
		if i.slashpath, err = i.run("mktemp -d --suffix _deployer_rootfs"); err != nil {
			err = utils.FormatError(err)
			return
		}
		if err = i.client.Attach(i.slashpath, i.localmount); err != nil {
			err = utils.FormatError(err)
			return
		}
		if err = setUtilNewPaths(i, bins); err != nil {
			err = utils.FormatError(err)
			return
		}
		qemuImgError = "please install qemu-img on remote host"
	}

	if config.Type != StorageTypeRAW {
		if _, err = i.run("which qemu-img"); err != nil {
			err = utils.FormatError(errors.New(qemuImgError))
			return
		}
		// set temporary name
		config.Path = strings.Replace(config.Path, "."+string(config.Type), "", -1)
	}

	i.config = config
	i.config.Path = config.Path + ".raw"
	if _, err = i.run("ls" + config.Path); err != nil {
		if err = i.create(); err != nil {
			err = utils.FormatError(err)
			return
		}
		if config.Partitions != nil {
			i.needToFormat = true
		}
	}

	i.loopDevice = new(loopDevice)
	i.loopDevice.amountOfMappers = 0
	return
}

func setUtilNewPaths(i *image, u *Utils) error {
	dir, err := utils.UploadBinaries(i.client.Config.Common, u.Kpartx)
	if err != nil {
		return utils.FormatError(err)
	}
	i.utils = new(Utils)
	i.utils.Kpartx = filepath.Join(dir, filepath.Base(u.Kpartx))
	i.utils.dir = dir
	return nil
}

// Parse processes RAW image
// Returns error/nil
func (i *image) Parse() error {
	var err error
	if i.loopDevice.name, err = i.bind(i.config.Path); err != nil {
		return utils.FormatError(err)
	}
	if i.needToFormat {
		if err := i.partTableMakefs(); err != nil {
			return utils.FormatError(err)
		}
	} else if i.config.Partitions != nil {
		if err := i.addMappers(); err != nil {
			return utils.FormatError(err)
		}
	}
	return nil
}

// Customize intended for the target customization
// - pathToConfigDir - path to directory containing configuration XML files
func (i *image) Customize(pathToConfigDir string) error {
	if i.amountOfMappers == 0 {
		return utils.FormatError(errors.New("amount of mappers is 0.Seems Parse() hasn't been called yet."))
	}
	return content.Customize(i.slashpath, pathToConfigDir)
}

// Release is trying to release the image correctly
// by unmounting the mappers in reverse order
// and the cleans up a temporary stuff
// Returns error or nil
func (i *image) Cleanup() error {
	if i.localmount != "" {
		if err := i.client.Detach(i.localmount); err != nil {
			return utils.FormatError(err)
		}
	}
	// Release registered mount points
	var index uint8 = i.loopDevice.amountOfMappers - 1
	for i.loopDevice.amountOfMappers != 0 {
		if i.loopDevice.mappers[index].mountPoint != "/" {
			if out, err := i.run(fmt.Sprintf("umount -l %s", i.loopDevice.mappers[index].mountPoint)); err != nil {
				return utils.FormatError(fmt.Errorf("%s [%v]", out, err))
			}
			i.loopDevice.amountOfMappers--
			index--
		}
	}
	if out, err := i.run(fmt.Sprintf("umount -l %s", i.slashpath)); err != nil {
		return utils.FormatError(fmt.Errorf("%s [%v]", out, err))
	}
	// unbind mappers and image
	if out, err := i.run(i.utils.Kpartx + " -d " + i.loopDevice.name); err != nil {
		return utils.FormatError(fmt.Errorf("%s [%v]", out, err))
	}
	if out, err := i.run("losetup -d " + i.loopDevice.name); err != nil {
		return utils.FormatError(fmt.Errorf("%s [%v]", out, err))
	}
	// remove mount point
	if out, err := i.run("rm -rf " + i.slashpath); err != nil {
		return utils.FormatError(fmt.Errorf("%s [%v]", out, err))
	}
	if i.localmount != "" {
		// remove mount point
		if out, err := i.run("rm -rf " + i.utils.dir); err != nil {
			return utils.FormatError(fmt.Errorf("%s [%v]", out, err))
		}
		// remove mount point
		if out, err := i.run("rm -rf " + i.localmount); err != nil {
			return utils.FormatError(fmt.Errorf("%s [%v]", out, err))
		}
	}
	return nil
}

// MakeBootable is responsible for making RAW disk bootable.
// The target disk could be either local or remote image
func (i *image) MakeBootable() error {
	switch i.config.BootLoader {
	case BootLoaderGrub:
		grubPath, err := i.run("chroot " + i.slashpath + "  which grub")
		if err != nil {
			return utils.FormatError(err)
		}
		cmd := fmt.Sprintf("echo -e \"device (hd0) %s\nroot (hd0,0)\nsetup (hd0)\n\"|%s",
			i.config.Path, filepath.Join(i.slashpath, grubPath))
		out, err := i.run(cmd)
		if err != nil {
			return utils.FormatError(fmt.Errorf("%s [%v]", out, err))
		}
		if strings.Contains(out, "Error 15: File not found") {
			return utils.FormatError(errors.New("installing GRUB [Error 15: File not found]"))
		}

	case BootLoaderGrub2:
		dummyLoopDevice, err := i.bind(i.loopDevice.mappers[0].name)
		if err != nil {
			return utils.FormatError(err)
		}
		defer i.run("losetup -d " + dummyLoopDevice)

		dummyLoopDeviceMp, err := i.run("mktemp -d --suffix _deployer_dummy_loop")
		if err != nil {
			return utils.FormatError(err)
		}
		if out, err := i.run("mount " + dummyLoopDevice + " " + dummyLoopDeviceMp); err != nil {
			return utils.FormatError(fmt.Errorf("%s [%v]", out, err))
		}
		defer func() {
			cmd := "umount -l " + dummyLoopDeviceMp + "/proc " + dummyLoopDeviceMp + "/dev;"
			cmd += "umount -f " + dummyLoopDeviceMp
			cmd += ";rm -rf " + dummyLoopDeviceMp
			i.run(cmd)
		}()

		cmd := fmt.Sprintf("mkdir -p %s/boot/grub; echo -e \"(hd0) %s\n(hd0,1) %s\" > %s/boot/grub/device.map;",
			dummyLoopDeviceMp, i.loopDevice.name, dummyLoopDevice, dummyLoopDeviceMp)
		cmd += "mount --bind /dev " + dummyLoopDeviceMp + "/dev ;chroot " + dummyLoopDeviceMp + " mount -t proc none /proc;"
		cmd += "chroot " + dummyLoopDeviceMp + " grub-install --no-floppy --grub-mkdevicemap=/boot/grub/device.map " + i.loopDevice.name
		cmd += ";chroot " + dummyLoopDeviceMp + " update-grub;"
		cmd += "rm -f " + dummyLoopDeviceMp + "/boot/grub/device.map;"
		cmd += "sed -i '/loop/d' " + dummyLoopDeviceMp + "/boot/grub/grub.cfg"

		if out, err := i.run(cmd); err != nil {
			return utils.FormatError(fmt.Errorf("%s [%v]", out, err))
		}

	case BootLoaderExtlinux:
		if _, err := i.run("chroot " + i.slashpath + " which extlinux"); err != nil {
			return utils.FormatError(fmt.Errorf("%s [%v]", "Extlinux not found", err))
		}

		defer func() {
			i.run("umount -l " + i.slashpath + "/proc " + i.slashpath + "/dev")
		}()

		var extlinuxMbrPath string
		if _, err := i.run("ls " + i.slashpath + "/usr/lib/EXTLINUX/mbr.bin"); err == nil {
			extlinuxMbrPath = "/usr/lib/EXTLINUX/mbr.bin"
		} else if _, err := i.run("ls " + i.slashpath + "/usr/lib/extlinux/mbr.bin"); err == nil {
			extlinuxMbrPath = "/usr/lib/extlinux/mbr.bin"
		} else {
			return utils.FormatError(errors.New("Extlinux mbr binary not found"))
		}

		cmd := "mount --bind /dev " + i.slashpath + "/dev;"
		cmd += "chroot " + i.slashpath + " /bin/bash -c "
		cmd += "\"LC_ALL=C export PATH=/usr/local/bin:/usr/local/sbin:/usr/bin:/usr/sbin:/bin:/sbin;"
		cmd += "mount -t proc none /proc;"
		cmd += "dd if=" + extlinuxMbrPath + " of=" + i.loopDevice.name + " bs=440 count=1 conv=notrunc;"
		cmd += "extlinux -i /boot/extlinux;"
		cmd += "extlinux-update || true\""
		if out, err := i.run(cmd); err != nil {
			return utils.FormatError(fmt.Errorf("%s [%v]", out, err))
		}
	}
	return nil
}

func (i *image) Convert() error {
	if i.config.Type != StorageTypeRAW {
		if err := i.convert(); err != nil {
			return utils.FormatError(err)
		}
	}
	return nil
}

// ReleaseOnInterrupt is trying to release appropriate image
// in case SIGHUP, SIGINT or SIGTERM signal received
func (i *image) ReleaseOnInterrupt() {
	//create a channel for interrupt handler
	interrupt := make(chan os.Signal, 1)
	// create an interrupt handler
	signal.Notify(interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)
	// run a seperate goroutine.
	go func() {
		for {
			select {
			case <-interrupt:
				i.Cleanup()
			}
		}
	}()
}

// Exports amount of mappers
func (i *image) AmountOfMappers() uint8 {
	return i.amountOfMappers
}

/// Private stuff ///

// partTable creates partition table on the RAW disk
func (i *image) partTableMakefs() error {
	if i.config.FdiskCmd == "" {
		i.generateFdiskCmd()
	}

	i.run(fmt.Sprintf("echo -e  \"%s\"|%s %s", i.config.FdiskCmd, "fdisk", i.loopDevice.name))
	mappers, err := i.getMappers(i.loopDevice.name)
	if err != nil {
		return utils.FormatError(err)
	}
	// first iteration - find root mount point and mount it
	for index, part := range i.config.Partitions {
		if part.MountPoint == "/" {
			cmd := fmt.Sprintf("mkfs -t %v -L %s %s %s", part.FileSystem,
				part.Label, part.FileSystemArgs, mappers[index])
			if out, err := i.run(cmd); err != nil {
				return utils.FormatError(fmt.Errorf("%s [%v]", out, err))
			}
			if out, err := i.run(fmt.Sprintf("mount %s %s", mappers[index], i.slashpath)); err != nil {
				return utils.FormatError(fmt.Errorf("%s [%v]", out, err))
			}
		}
	}
	// second iteration - treat everything else except / and SWAP
	for index, part := range i.config.Partitions {
		mapper := mappers[index]
		if part.Type == 82 {
			if out, err := i.run(fmt.Sprintf("mkswap -L %s %s", part.Label, mapper)); err != nil {
				return utils.FormatError(fmt.Errorf("%s [%v]", out, err))
			}
		} else if part.MountPoint != "/" {
			cmd := fmt.Sprintf("mkfs -t %v -L %s %s %s", part.FileSystem,
				part.Label, part.FileSystemArgs, mapper)
			if out, err := i.run(cmd); err != nil {
				return utils.FormatError(fmt.Errorf("%s [%v]", out, err))
			}
			if err := i.addMapper(mapper, part.MountPoint); err != nil {
				return utils.FormatError(err)
			}
		}
	}
	return nil
}

const (
	// calculate partition size in percents
	calcInPercents = -1
	// allocate all the space left on the disk
	allocateAll = -2
)

// generateFdiskCmd constructs a command to be used by fdisk utility
// to create partition table on our disk
func (i *image) generateFdiskCmd() {
	// extended partiotion sequence number
	extendedPart := 4
	// first logical drive
	logicalDrive := 5
	// total amount of partitions
	amountOfPartitions := len(i.config.Partitions)

	// header of the command
	i.config.FdiskCmd = `o\n`
	for index, part := range i.config.Partitions {
		// in case partition size in megabytes is set to -1
		// caclulate partition size in percents
		if part.SizeMb == calcInPercents {
			if part.SizePercents == allocateAll {
				part.SizeMb = allocateAll
			} else {
				part.SizeMb = i.config.SizeMb / 100 * part.SizePercents
			}
		}
		switch {
		// in case we are treating the last partition and need to allocate all the space left for it
		case part.Sequence == amountOfPartitions && part.SizeMb == allocateAll:
			if part.Sequence > extendedPart {
				i.config.FdiskCmd += fmt.Sprintf(`n\nl\n%d\n\n\n`, logicalDrive)
				logicalDrive++
			} else if part.Sequence < extendedPart {
				i.config.FdiskCmd += fmt.Sprintf(`n\np\n%d\n\n\n`, part.Sequence)
			} else if part.Sequence == extendedPart {
				i.config.FdiskCmd += `n\np\n\n\n`
			}
		// in case we are treating a primary partition
		case part.Sequence < extendedPart:
			i.config.FdiskCmd += fmt.Sprintf(`n\np\n%d\n\n+%dM\n`, part.Sequence, part.SizeMb)
		// in case partition sequence is 4 - create extended partiton and treat the partition
		// with sequence 4 as a logical drive
		case part.Sequence == extendedPart:
			i.config.FdiskCmd += fmt.Sprintf(`n\ne\n\n\n\n+%dM\n`, part.SizeMb)
		// default behaviour
		default:
			i.config.FdiskCmd += fmt.Sprintf(`n\n\n+%dM\n`, part.SizeMb)
		}
		if index == 0 {
			i.config.FdiskCmd += fmt.Sprintf(`t\n%d\n`, part.Type)
		} else {
			i.config.FdiskCmd += fmt.Sprintf(`t\n%d\n%d\n`, part.Sequence, part.Type)
		}
	}

	if i.config.Bootable {
		i.config.FdiskCmd += fmt.Sprintf(`a\n%d\n`, i.config.ActivePartition)
	}

	// write changes
	i.config.FdiskCmd += `w`
}

// addMappers registers appropriate mappers
func (i *image) addMappers() error {
	mappers, err := i.getMappers(i.loopDevice.name)
	if err != nil {
		return utils.FormatError(err)
	}
	// first iteration - find root mount point and mount it
	for index, part := range i.config.Partitions {
		if part.MountPoint == "/" {
			if out, err := i.run(fmt.Sprintf("mount %s %s", mappers[index], i.slashpath)); err != nil {
				return utils.FormatError(fmt.Errorf("%s [%v]", out, err))
			}
		}
	}
	// second iteration - treat everything else except / and SWAP
	for index, part := range i.config.Partitions {
		// create SWAP and do not add to the mappers slice
		if part.Type != 82 && part.MountPoint != "/" {
			if err := i.addMapper(mappers[index], part.MountPoint); err != nil {
				return utils.FormatError(err)
			}
		}
	}
	return nil
}

// addMapper registers appropriate mapper and it's mount point
func (i *image) addMapper(mapperDeviceName, path string) error {
	mountPoint := filepath.Join(i.slashpath, path)
	if out, err := i.run("mkdir -p " + mountPoint); err != nil {
		return utils.FormatError(fmt.Errorf("%s [%v]", out, err))
	}
	// check if the volume is already mounted
	mounted, err := isMounted(mapperDeviceName)
	if err != nil {
		return utils.FormatError(err)
	}
	if !mounted {
		if out, err := i.run(fmt.Sprintf("mount %s %s", mapperDeviceName, mountPoint)); err != nil {
			return utils.FormatError(fmt.Errorf("%s [%v]", out, err))
		}
	}
	// add mapper
	i.mappers = append(i.mappers,
		&mapperDevice{
			name:       mapperDeviceName,
			mountPoint: mountPoint,
		},
	)
	// advance amount of mappers
	i.loopDevice.amountOfMappers++
	return nil
}

// create is intended for creating RAW image
func (i *image) create() error {
	out, err := i.run(fmt.Sprintf("dd if=/dev/zero of=%s count=1 bs=1 seek=%vM", i.config.Path, i.config.SizeMb))
	if err != nil {
		return utils.FormatError(fmt.Errorf("%s [%v]", out, err))
	}
	return nil
}

// getMappers is responsible for finding mappers bound to appropriate loop device
// and providing the stuff as a slice
func (i *image) getMappers(loopDeviceName string) ([]string, error) {
	var mappers []string
	if _, err := i.run(i.utils.Kpartx + " -a " + loopDeviceName); err != nil {
		return mappers, utils.FormatError(err)
	}
	// somehow on RHEL based systems refresh might take some time therefore
	// no mappers are available until then
	duration, err := time.ParseDuration("1s")
	if err != nil {
		return mappers, utils.FormatError(err)
	}
	time.Sleep(duration)

	cmd := fmt.Sprintf("find /dev -name loop%sp[0-9]",
		strings.TrimSpace(strings.SplitAfter(loopDeviceName, "/dev/loop")[1]))
	out, err := i.run(cmd)
	if err != nil {
		return mappers, utils.FormatError(fmt.Errorf("%s [%v]", out, err))
	}
	for _, line := range strings.Split(out, "\n") {
		if line != "" {
			mappers = append(mappers, line)
		}
	}
	if len(mappers) == 0 {
		return mappers, utils.FormatError(errors.New("mappers not found"))
	}
	sort.Strings(mappers)
	return mappers, nil
}

// convert is responsible for converting RAW image to other format
func (i *image) convert() error {
	// set the new path - append extention
	newPath := fmt.Sprintf("%s.%s", strings.TrimSuffix(i.config.Path, ".raw"), i.config.Type)
	if out, err := i.run(fmt.Sprintf("qemu-img convert -f raw -O %s %s %s", i.config.Type, i.config.Path, newPath)); err != nil {
		return utils.FormatError(fmt.Errorf("%s [%v]", out, err))
	}
	//remove temporary image
	if out, err := i.run("rm -rf " + i.config.Path); err != nil {
		return utils.FormatError(fmt.Errorf("%s [%v]", out, err))
	}
	// expose the new path
	i.config.Path = newPath
	return nil
}

func (i *image) bind(imagePath string) (loopDevice string, err error) {
	loopDevice, err = i.run("losetup -f")
	if err != nil {
		err = utils.FormatError(err)
		return
	}
	cmd := fmt.Sprintf("losetup %s %s", loopDevice, imagePath)
	if out, er := i.run(cmd); err != nil {
		err = utils.FormatError(fmt.Errorf("%s [%v]", out, er))
		return
	}
	return
}
