package docker

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"time"

	"github.com/anchore/stereoscope/internal/bus"
	"github.com/anchore/stereoscope/internal/docker"
	"github.com/anchore/stereoscope/internal/log"
	"github.com/anchore/stereoscope/pkg/image"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/wagoodman/go-partybus"
	"github.com/wagoodman/go-progress"
)

type DaemonImageProvider struct {
	ImageRef name.Reference
	cacheDir string
}

func NewProviderFromDaemon(imgRef name.Reference, cacheDir string) *DaemonImageProvider {
	return &DaemonImageProvider{
		ImageRef: imgRef,
		cacheDir: cacheDir,
	}
}

func (p *DaemonImageProvider) Provide() (*image.Image, error) {
	// create a file within the temp dir
	tempTarFile, err := os.Create(path.Join(p.cacheDir, "image.tar"))
	if err != nil {
		return nil, fmt.Errorf("unable to create temp file for image: %w", err)
	}
	defer func() {
		err := tempTarFile.Close()
		if err != nil {
			log.Errorf("unable to close temp file (%s): %w", tempTarFile.Name(), err)
		}
	}()

	// fetch the image from the docker daemon
	dockerClient, err := docker.GetClient()
	if err != nil {
		return nil, fmt.Errorf("unable to get docker client: %w", err)
	}

	// fetch the expected image size to estimate and measure progress
	inspect, _, err := dockerClient.ImageInspectWithRaw(context.Background(), p.ImageRef.Name())
	if err != nil {
		return nil, fmt.Errorf("unable to inspect image: %w", err)
	}

	// docker image save clocks in at ~150MB/sec on my laptop... milage may vary, of course :shrug:
	mb := math.Pow(2, 20)
	sec := float64(inspect.VirtualSize) / (mb * 150)
	approxSaveTime := time.Duration(sec*1000) * time.Millisecond

	dummyProgress := progress.NewTimedProgress(approxSaveTime)
	copyProgress := progress.NewSizedWriter(inspect.VirtualSize)
	aggregateProgress := progress.NewAggregateGenerator(dummyProgress, copyProgress)
	aggregateProgress.SetStrategy(progress.NormalizeStrategy)

	// let consumers know of a monitorable event (image save + copy)
	bus.Publish(partybus.Event{
		Type:  "save-image",
		Value: progress.Progressor(aggregateProgress),
	})

	// fetch the image from the docker daemon
	readCloser, err := dockerClient.ImageSave(context.Background(), []string{p.ImageRef.Name()})
	if err != nil {
		return nil, fmt.Errorf("unable to save image tar: %w", err)
	}
	defer func() {
		err := readCloser.Close()
		if err != nil {
			log.Errorf("unable to close temp file (%s): %w", tempTarFile.Name(), err)
		}
	}()

	// cancel indeterminate progress
	dummyProgress.SetCompleted()

	// save the image contents to the temp file
	// note: this is the same image that will be used to querying image content during analysis
	nBytes, err := io.Copy(io.MultiWriter(tempTarFile, copyProgress), readCloser)
	if err != nil {
		return nil, fmt.Errorf("unable to save image to tar: %w", err)
	}
	if nBytes == 0 {
		return nil, fmt.Errorf("cannot provide an empty image")
	}

	// use the tar utils to load a v1.Image from the tar file on disk
	img, err := tarball.ImageFromPath(tempTarFile.Name(), nil)
	if err != nil {
		return nil, err
	}

	tags, err := extractTags(tempTarFile.Name())
	if err != nil {
		return nil, err
	}

	return image.NewImageWithTags(img, tags), nil
}
