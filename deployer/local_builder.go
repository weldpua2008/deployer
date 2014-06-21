package deployer

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/dorzheh/infra/image"
	"github.com/dorzheh/infra/utils"
)

type Builder interface {
	Run() (Artifact, error)
}

type ImageBuilder struct {
	ImagePath   string
	RootfsMp    string
	ImageConfig *image.Topology
	Filler      image.Rootfs
	Compress    bool
}

func (b *ImageBuilder) Run() (Artifact, error) {
	if err := utils.CreateDirRecursively(b.RootfsMp, 0755, 0, 0, false); err != nil {
		return nil, err
	}
	defer os.RemoveAll(b.RootfsMp)
	// create new image object
	img, err := image.New(b.ImagePath, b.RootfsMp, b.ImageConfig)
	if err != nil {
		return nil, err
	}
	// parse the image
	if err := img.Parse(); err != nil {
		return nil, err
	}
	// interrupt handler
	img.ReleaseOnInterrupt()
	defer func() {
		if err := img.Release(); err != nil {
			panic(err)
		}
	}()
	// create and customize rootfs
	if b.Filler != nil {
		if err := b.Filler.MakeRootfs(b.RootfsMp); err != nil {
			return nil, err
		}
		// install application.
		if err := b.Filler.InstallApp(b.RootfsMp); err != nil {
			return nil, err
		}
	}

	origName := filepath.Base(b.ImagePath)
	if b.Compress {
		newImagePath, err := compressArtifact(b.ImagePath)
		if err != nil {
			return nil, err
		}
		if err := os.Remove(b.ImagePath); err != nil {
			return nil, err
		}
		b.ImagePath = newImagePath
	}
	return &LocalArtifact{
		Name: origName,
		Path: b.ImagePath,
		Type: ImageArtifact,
	}, nil
}

type MetadataBuilder struct {
	Source   string
	Dest     string
	UserData interface{}
	Compress bool
}

func (b *MetadataBuilder) Run() (Artifact, error) {
	f, err := ioutil.ReadFile(b.Source)
	if err != nil {
		return nil, err
	}
	data, err := ProcessTemplate(string(f), b.UserData)
	if err != nil {
		return nil, err
	}
	if err := ioutil.WriteFile(b.Dest, data, 0644); err != nil {
		return nil, err
	}

	origName := filepath.Base(b.Dest)
	if b.Compress {
		newDest, err := compressArtifact(b.Dest)
		if err != nil {
			return nil, err
		}
		if err := os.Remove(b.Dest); err != nil {
			return nil, err
		}
		b.Dest = newDest
	}
	return &LocalArtifact{
		Name: origName,
		Path: b.Dest,
		Type: MetadataArtifact,
	}, nil
}

type LocalInstanceBuilder struct {
	Filler image.Rootfs
}

func (b *LocalInstanceBuilder) Run() (a Artifact, err error) {
	if err = b.Filler.MakeRootfs("/"); err != nil {
		return
	}
	if err = b.Filler.InstallApp("/"); err != nil {
		return
	}
	return
}

func compressArtifact(path string) (string, error) {
	dir := filepath.Dir(path)
	oldArtifactFile := filepath.Base(path)
	if err := os.Chdir(dir); err != nil {
		return "", err
	}
	newArtifactFile := oldArtifactFile + ".tgz"
	var stderr bytes.Buffer
	cmd := exec.Command("tar", "cfzp", newArtifactFile, oldArtifactFile)
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", err
	}
	if err := cmd.Wait(); err != nil {
		return "", fmt.Errorf("%s [%s]", stderr.String(), err)
	}
	return filepath.Join(dir, newArtifactFile), nil
}
