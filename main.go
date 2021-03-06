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
	"net/http"
	"os"
	"path/filepath"
	"strings"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	dockercli "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/pkg/errors"
	"github.com/spf13/pflag"
	"golang.org/x/crypto/ssh/terminal"
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

func stage(imageRef, baseImageRef, stack, appPath string, buildpacks []string) error {
	client, err := NewClient()

	builderPath := "/lifecycle/builder"
	launcherPath := "/lifecycle/launcher"
	if strings.HasPrefix(stack, "windows") {
		builderPath += ".exe"
		launcherPath += ".exe"
	}
	var cacheTarFile string
	for _, dir := range []string{"TEMP", "TMPDIR", "HOME", "HOMEPATH"} {
		if os.Getenv(dir) != "" {
			cacheTarFile = filepath.Join(os.Getenv(dir), fmt.Sprintf("cfwindowsstager.%x.tar", md5.Sum([]byte(imageRef))))
			break
		}
	}

	ctx := context.Background()

	rc, err := client.ImagePull(ctx, baseImageRef, dockertypes.ImagePullOptions{All: false})
	if err != nil {
		return errors.Wrap(err, "container create")
	}
	defer rc.Close()
	if err := jsonmessage.DisplayJSONMessagesStream(rc, os.Stdout, os.Stdout.Fd(), terminal.IsTerminal(int(os.Stdout.Fd())), nil); err != nil {
		return err
	}

	stageCmd := []string{
		builderPath,
		"-buildDir=/home/vcap/app",
		"-buildpacksDir=/buildpacks",
		"-outputDroplet=/tmp/droplet",
		"-outputMetadata=/tmp/result.json",
		"-buildpackOrder=" + strings.Join(buildpacks, ","),
		"-buildArtifactsCacheDir=/tmp/cache",
		// "-skipCertVerify", // skip SSL certificate verification
	}
	if len(buildpacks) >= 2 {
		stageCmd = append(stageCmd, "-skipDetect") // Multi buildpack mode
	}

	ctr, err := client.ContainerCreate(ctx, &container.Config{
		Image:      baseImageRef,
		Cmd:        stageCmd,
		Env:        []string{"CF_STACK=" + stack},
		WorkingDir: "/home/vcap",
	}, nil, nil, "")
	if err != nil {
		return errors.Wrap(err, "container create")
	}
	defer client.ContainerRemove(ctx, ctr.ID, dockertypes.ContainerRemoveOptions{})

	if err := CopyLifecycleToContainer(client, ctx, ctr.ID); err != nil {
		return err
	}

	for _, dir := range []string{"/buildpacks", "/home/vcap/app", "/tmp"} {
		if err := MakeDirInContainer(client, ctx, ctr.ID, dir); err != nil {
			return err
		}
	}

	if err := CopyBuildpacksToContainer(client, ctx, ctr.ID, buildpacks); err != nil {
		return errors.Wrap(err, "copy local buildpacks to container")
	}

	if cacheTarFile != "" {
		if err := CopyCacheToContainer(client, ctx, ctr.ID, cacheTarFile); err != nil {
			return errors.Wrap(err, "copy cache to container")
		}
	}

	tr, err := archive.TarWithOptions(appPath, &archive.TarOptions{})
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

	ctr2, err := client.ContainerCreate(ctx, &container.Config{
		Image: baseImageRef,
		Cmd:   []string{launcherPath, "/home/vcap/app", startCommand, ""},
		Env: []string{
			"PORT=8080",
			"VCAP_APP_HOST=0.0.0.0",
			"VCAP_APP_PORT=8080",
			"CF_STACK=" + stack,
		},
		WorkingDir: "/home/vcap",
		ExposedPorts: nat.PortSet{
			"8080": struct{}{},
		},
	}, nil, nil, "")
	if err != nil {
		return errors.Wrap(err, "create container to commit")
	}
	defer client.ContainerRemove(ctx, ctr2.ID, dockertypes.ContainerRemoveOptions{})

	if err := CopyLifecycleToContainer(client, ctx, ctr2.ID); err != nil {
		return err
	}

	if err := CopyDropletToContainer(client, ctx, ctr.ID, ctr2.ID); err != nil {
		return errors.Wrap(err, "copy droplet to base container")
	}

	if cacheTarFile != "" {
		if rc, _, err := client.CopyFromContainer(ctx, ctr.ID, "/tmp/cache"); err != nil {
			return err
		} else {
			defer rc.Close()
			f, err := os.Create(cacheTarFile)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, rc); err != nil {
				return err
			}
		}
	}

	if _, err := client.ContainerCommit(ctx, ctr2.ID, dockertypes.ContainerCommitOptions{
		Reference: imageRef,
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

	if err := MakeDirInContainer(client, ctx, destID, "/home/vcap"); err != nil {
		return err
	}

	if err := client.CopyToContainer(ctx, destID, "/home/vcap", tr, dockertypes.CopyToContainerOptions{
		AllowOverwriteDirWithFile: true,
	}); err != nil {
		return err
	}

	return nil
}

func MakeDirInContainer(client *dockercli.Client, ctx context.Context, ctrID string, dir string) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{Typeflag: tar.TypeDir, Name: dir, Mode: int64(0755)}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	tw.Close()

	return client.CopyToContainer(ctx, ctrID, "/", &buf, dockertypes.CopyToContainerOptions{})
}

func fileExists(path string) (bool, error) {
	if _, err := os.Stat("/path/to/whatever"); err == nil {
		return true, nil
	} else if os.IsNotExist(err) {
		return false, nil
	} else {
		return false, err
	}
}

func CopyLifecycleToContainer(client *dockercli.Client, ctx context.Context, ctrID string) error {
	lifecyclePath := ".cfwindowsstager.lifecycle.tar.gz"
	for _, dir := range []string{"TEMP", "TMPDIR", "HOME", "HOMEPATH"} {
		if os.Getenv(dir) != "" {
			lifecyclePath = filepath.Join(os.Getenv(dir), lifecyclePath)
			break
		}
	}

	if exists, err := fileExists(lifecyclePath); err != nil {
		return errors.Wrap(err, "testing existence of lifecycle.tar.gz")
	} else if !exists {
		f, err := os.Create(lifecyclePath)
		if err != nil {
			return err
		}
		defer f.Close()
		res, err := http.Get("https://github.com/dgodd/cfwindowsstager/releases/download/v0.0.1/lifecycle.tar.gz")
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if _, err := io.Copy(f, res.Body); err != nil {
			return err
		}
	}

	rc, err := os.Open(lifecyclePath)
	if err != nil {
		return errors.Wrap(err, "open lifecycle.tar.gz from home")
	}
	defer rc.Close()
	if err := client.CopyToContainer(ctx, ctrID, "/", rc, dockertypes.CopyToContainerOptions{}); err != nil {
		return errors.Wrap(err, "copy lifecycle tar.gz to container")
	}
	return nil
}

func CopyBuildpacksToContainer(client *dockercli.Client, ctx context.Context, ctrID string, buildpacks []string) error {
	for _, bpPath := range buildpacks {
		if strings.HasPrefix(bpPath, "https://") || strings.HasPrefix(bpPath, "http://") {
			fmt.Println("Use online buildpack:", bpPath)
			continue
		}
		// TODO check file exists to give better error
		fmt.Println("Copy local buildpack", bpPath, "to container")
		pathMD5 := fmt.Sprintf("%x", md5.Sum([]byte(bpPath)))
		tr, err := ConvertZipToTar(bpPath, pathMD5+"/")
		if err != nil {
			return errors.Wrap(err, "tar buildpack before copying to container")
		}
		if err := client.CopyToContainer(ctx, ctrID, "/buildpacks/", tr, dockertypes.CopyToContainerOptions{}); err != nil {
			return errors.Wrap(err, "copy buildpacks to container")
		}
	}
	return nil
}

func CopyCacheToContainer(client *dockercli.Client, ctx context.Context, ctrID string, cacheTarFile string) error {
	f, err := os.Open(cacheTarFile)
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}

	return client.CopyToContainer(ctx, ctrID, "/tmp/", f, dockertypes.CopyToContainerOptions{})
}

func ConvertZipToTar(path, prefix string) (io.Reader, error) {
	// TODO use a pipe rather than in memory
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	for _, f := range r.File {
		hdr := &tar.Header{Name: prefix + f.Name, Size: int64(f.UncompressedSize64), Mode: int64(f.Mode())}
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
	var imageRef = pflag.String("image", "cfwindowsstager/myapp", "name of docker image to build")
	var baseImageRef = pflag.String("base", "cloudfoundry/windows2016fs:1803", "name of docker image base staging on, must contain lifecycle")
	var stack = pflag.String("stack", "windows2016", "name of stack for docker image")
	var appPath = pflag.String("app", ".", "path to app to push")
	var buildpacks = pflag.StringSlice("buildpack", []string{"https://github.com/cloudfoundry/hwc-buildpack/releases/download/v3.1.3/hwc-buildpack-windows2016-v3.1.3.zip"}, "buildpacks to use, either http url or local zip file")
	pflag.Parse()

	var err error
	*appPath, err = filepath.Abs(*appPath)
	if err != nil {
		fmt.Println("ERROR:", err)
		os.Exit(1)
	}

	if err := stage(*imageRef, *baseImageRef, *stack, *appPath, *buildpacks); err != nil {
		fmt.Println("ERROR:", err)
		os.Exit(1)
	}

	md5ImageRef := fmt.Sprintf("%x", md5.Sum([]byte(*imageRef)))
	fmt.Printf("\nTo run:\n  docker run --rm --name=%s -d -e PORT=8080 -p 8080:8080 %s\nThen to stop:\n  docker kill %s\n", md5ImageRef, *imageRef, md5ImageRef)
}
