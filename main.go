package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
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
	ctr, err := client.ContainerCreate(ctx, &container.Config{
		Image: "cflinuxfs3wbal",
		Cmd: []string{
			"/tmp/lifecycle/builder",
			"-buildDir=/home/vcap/app",
			"-buildpacksDir=/buildpacks",
			// "-buildArtifactsCacheDir=/tmp/cache",
			"-outputDroplet=/tmp/droplet",
			"-outputMetadata=/tmp/result.json",
			"-buildpackOrder=/Users/davegoddard/Downloads/ruby-buildpack-cflinuxfs3-v1.7.29.zip", // comma-separated list of buildpacks, to be tried in order
			// "-skipDetect",
			// "-skipCertVerify", // skip SSL certificate verification
		},
	}, nil, nil, "")
	if err != nil {
		return errors.Wrap(err, "container create")
	}
	defer client.ContainerRemove(ctx, ctr.ID, dockertypes.ContainerRemoveOptions{})

	if err := CopyBuildpacksToContainer(client, ctx, ctr.ID); err != nil {
		return errors.Wrap(err, "copy local buildpacks to container")
	}

	tr, err := archive.TarWithOptions("/Users/davegoddard/workspace/ruby-buildpack/fixtures/sinatra", &archive.TarOptions{})
	if err != nil {
		return errors.Wrap(err, "tar app before copying to container")
	}
	if err := client.CopyToContainer(ctx, ctr.ID, "/home/vcap/app", tr, dockertypes.CopyToContainerOptions{}); err != nil {
		return errors.Wrap(err, "copy app tar to container")
	}

	if err := RunContainer(client, ctx, ctr.ID, os.Stdout, os.Stderr); err != nil {
		return errors.Wrap(err, "container run")
	}

	startCommand, err := ResultJSONProcessType(client, ctx, ctr.ID, "/tmp/result.json")
	if err != nil {
		return errors.Wrap(err, "find start command")
	}
	fmt.Println("START COMMAND:", startCommand)

	// TODO expose port 8080
	ctr2, err := client.ContainerCreate(ctx, &container.Config{
		Image: "cflinuxfs3wbal",
		Cmd:   []string{"/tmp/lifecycle/launcher", "/home/vcap/app", startCommand, ""},
	}, &container.HostConfig{}, nil, "")
	if err != nil {
		return errors.Wrap(err, "create container to commit")
	}
	defer client.ContainerRemove(ctx, ctr2.ID, dockertypes.ContainerRemoveOptions{})

	if err := CopyDropletToContainer(client, ctx, ctr.ID, ctr2.ID); err != nil {
		return errors.Wrap(err, "copy droplet to base container")
	}

	if _, err := client.ContainerCommit(ctx, ctr2.ID, dockertypes.ContainerCommitOptions{
		Reference: "fixme/dave-app",
	}); err != nil {
		return errors.Wrap(err, "create image from container")
	}

	return nil
}

func ResultJSONProcessType(client *dockercli.Client, ctx context.Context, ctrID string, path string) (string, error) {
	rc, _, err := client.CopyFromContainer(ctx, ctrID, path)
	if err != nil {
		return "", errors.Wrap(err, fmt.Sprintf("copy single file (%s) out of container", path))
	}
	defer rc.Close()
	tr := tar.NewReader(rc)
	_, err = tr.Next()
	if err == io.EOF {
		return "", errors.New("result.json not found in container")
	}
	if err != nil {
		return "", errors.Wrap(err, "parsing result.json tar from container")
	}
	result := struct {
		ProcessTypes struct {
			Web string `json:"web"`
		} `json:"process_types"`
	}{}
	if err := json.NewDecoder(tr).Decode(&result); err != nil {
		return "", errors.Wrap(err, "parsing result.json from container")
	}
	return result.ProcessTypes.Web, nil
}

func CopyDropletToContainer(client *dockercli.Client, ctx context.Context, srcID, destID string) error {
	rc, _, err := client.CopyFromContainer(ctx, srcID, "/tmp/droplet")
	if err != nil {
		return err
	}
	defer rc.Close()
	tr := tar.NewReader(rc)
	_, err = tr.Next()
	if err != nil {
		return err
	}

	if err := client.CopyToContainer(ctx, destID, "/home/vcap", tr, dockertypes.CopyToContainerOptions{
		AllowOverwriteDirWithFile: true,
	}); err != nil {
		return err
	}

	return nil
}

func CopyBuildpacksToContainer(client *dockercli.Client, ctx context.Context, ctrID string) error {
	// tr, err := archive.TarWithOptions("/Users/davegoddard/Downloads/ruby-buildpack-cflinuxfs3-v1.7.29.zip", &archive.TarOptions{})
	bpPath := "/Users/davegoddard/Downloads/ruby-buildpack-cflinuxfs3-v1.7.29.zip"
	tr, err := ConvertZipToTar(bpPath)
	if err != nil {
		return errors.Wrap(err, "tar buildpack before copying to container")
	}
	if err := client.CopyToContainer(ctx, ctrID, "/buildpacks/", tr, dockertypes.CopyToContainerOptions{}); err != nil {
		return errors.Wrap(err, "copy buildpacks to container")
	}
	return nil
}

func ConvertZipToTar(path string) (io.Reader, error) {
	// TODO use a pipe rather than in memory
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	pathMD5 := fmt.Sprintf("%x", md5.Sum([]byte(path))) + "/"
	fmt.Println("BP PATH:", path, pathMD5)

	for _, f := range r.File {
		hdr := &tar.Header{Name: pathMD5 + f.Name, Size: int64(f.UncompressedSize64), Mode: int64(f.Mode())}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		_, err = io.CopyN(tw, rc, int64(f.UncompressedSize64))
		if err != nil {
			return nil, err
		}
	}

	return &buf, nil
}

func main() {
	if err := stage(); err != nil {
		fmt.Println("ERROR:", err)
		os.Exit(1)
	}
}
