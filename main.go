package main

import (
	"context"
	"fmt"
	"io"
	"os"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	dockercli "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/pkg/errors"
)

func NewClient() (*dockercli.Client, error) {
	cli, err := dockercli.NewClientWithOpts(dockercli.FromEnv, dockercli.WithVersion("1.38"))
	if err != nil {
		return nil, errors.Wrap(err, "new docker client")
	}
	return cli, nil
}

func RunContainer(d *dockercli.Client, ctx context.Context, id string, stdout io.Writer, stderr io.Writer) error {
	bodyChan, errChan := d.ContainerWait(ctx, id, container.WaitConditionNextExit)

	if err := d.ContainerStart(ctx, id, dockertypes.ContainerStartOptions{}); err != nil {
		return errors.Wrap(err, "container start")
	}
	logs, err := d.ContainerLogs(ctx, id, dockertypes.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return errors.Wrap(err, "container logs stdout")
	}

	copyErr := make(chan error)
	go func() {
		_, err := stdcopy.StdCopy(stdout, stderr, logs)
		copyErr <- err
	}()

	select {
	case body := <-bodyChan:
		if body.StatusCode != 0 {
			return fmt.Errorf("failed with status code: %d", body.StatusCode)
		}
	case err := <-errChan:
		return err
	}
	return <-copyErr
}

func stage() error {
	client, err := NewClient()

	ctx := context.Background()
	ctr, err := client.ContainerCreate(
		ctx,
		&container.Config{
			Image: "cflinuxfs3wbal",
			Cmd: []string{
				"/tmp/lifecycle/builder",
				"-buildDir=/app",
				"-buildpacksDir=/tmp/buildpacks",
				"-outputDroplet=/tmp/droplet",
				"-outputMetadata=/tmp/result.json",
				// "-skipDetect",
				"-buildpackOrder=https://github.com/cloudfoundry/ruby-buildpack/releases/download/v1.7.29/ruby-buildpack-cflinuxfs3-v1.7.29.zip", // comma-separated list of buildpacks, to be tried in order

				// -buildArtifactsCacheDir string
				// directory where previous cached build artifacts should be extracted (default "/tmp/cache")
				// -skipCertVerify
				// skip SSL certificate verification
			},
		},
		nil, // &container.HostConfig{
		// 	Binds: []string{
		// 		fmt.Sprintf("%s:%s:", b.CacheVolume, launchDir),
		// 	},
		nil, "")
	if err != nil {
		return errors.Wrap(err, "container create")
	}
	defer client.ContainerRemove(ctx, ctr.ID, dockertypes.ContainerRemoveOptions{})
	fmt.Println("CTR ID:", ctr.ID)

	tr, err := archive.TarWithOptions("/Users/davegoddard/workspace/ruby-buildpack/fixtures/sinatra", &archive.TarOptions{})
	if err != nil {
		return errors.Wrap(err, "tar app before copying to container")
	}
	if err := client.CopyToContainer(ctx, ctr.ID, "/app", tr, dockertypes.CopyToContainerOptions{}); err != nil {
		return errors.Wrap(err, "copy app tar to container")
	}

	if err := RunContainer(client, ctx, ctr.ID, os.Stdout, os.Stderr); err != nil {
		return errors.Wrap(err, "container run")
	}

	return nil
}

func main() {
	if err := stage(); err != nil {
		panic(err)
	}
}
