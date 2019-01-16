package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	dockercli "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/pkg/errors"
)

// COPIED ; TODO de-duplicate
func NewClient() (*dockercli.Client, error) {
	cli, err := dockercli.NewClientWithOpts(dockercli.FromEnv, dockercli.WithVersion("1.38"))
	if err != nil {
		return nil, errors.Wrap(err, "new docker client")
	}
	return cli, nil
}

// COPIED ; TODO de-duplicate
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

func main() {
	client, err := NewClient()
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	ctr, err := client.ContainerCreate(ctx, &container.Config{
		Image: "golang",
		Cmd: []string{"bash", "-c", `
		set -eux
		git clone --depth=1 https://github.com/cloudfoundry/buildpackapplifecycle.git /buildpackapplifecycle && \
		cd /buildpackapplifecycle/ && \
		go mod init
		export CGO_ENABLED=0
		go build -o /lifecycle/builder ./builder/
		go build -o /lifecycle/launcher ./launcher/
		export GOOS=windows
		go build -o /lifecycle/getenv.exe ./getenv/
		go build -o /lifecycle/launcher.exe ./launcher/
		go build -o /lifecycle/builder.exe ./builder/
		ls -l /lifecycle/
		`},
	}, &container.HostConfig{
		Binds: []string{
			"cfwindowsstager_lifecycle_build_pkgs:/go/pkg/mod",
		},
	}, nil, "")
	if err != nil {
		panic(err)
	}
	defer client.ContainerRemove(ctx, ctr.ID, dockertypes.ContainerRemoveOptions{})

	if err := RunContainer(client, ctx, ctr.ID, os.Stdout, os.Stderr); err != nil {
		panic(err)
	}

	rc, _, err := client.CopyFromContainer(ctx, ctr.ID, "/lifecycle/")
	if err != nil {
		// return errors.Wrap(err, fmt.Sprintf("copy single file (%s) out of container", "/lifecycle"))
		panic(err)
	}
	defer rc.Close()

	f, err := os.Create(filepath.Join(os.Getenv("HOME"), ".cfwindowsstager.lifecycle.tar"))
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if _, err := io.Copy(f, rc); err != nil {
		panic(err)
	}
}
