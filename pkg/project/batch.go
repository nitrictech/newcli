package project

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/nitrictech/cli/pkg/docker"
	"github.com/nitrictech/cli/pkg/netx"
	"github.com/nitrictech/cli/pkg/project/runtime"
	"github.com/nitrictech/nitric/core/pkg/logger"
	"github.com/samber/lo"
	"github.com/spf13/afero"
)

type Batch struct {
	Name string

	// filepath relative to the project root directory
	filepath     string
	buildContext runtime.RuntimeBuildContext

	runCmd string
}

func (s *Batch) GetFilePath() string {
	return s.filepath
}

func (s *Batch) GetAbsoluteFilePath() (string, error) {
	return filepath.Abs(s.filepath)
}

// FIXME: Duplicate code from service.go
func (s *Batch) BuildImage(fs afero.Fs, logs io.Writer) error {
	dockerClient, err := docker.New()
	if err != nil {
		return err
	}

	err = fs.MkdirAll(tempBuildDir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("unable to create temporary build directory %s: %w", tempBuildDir, err)
	}

	tmpDockerFile, err := afero.TempFile(fs, tempBuildDir, fmt.Sprintf("%s-*.dockerfile", s.Name))
	if err != nil {
		return fmt.Errorf("unable to create temporary dockerfile for service %s: %w", s.Name, err)
	}

	if err := afero.WriteFile(fs, tmpDockerFile.Name(), []byte(s.buildContext.DockerfileContents), os.ModePerm); err != nil {
		return fmt.Errorf("unable to write temporary dockerfile for service %s: %w", s.Name, err)
	}

	defer func() {
		tmpDockerFile.Close()

		err := fs.Remove(tmpDockerFile.Name())
		if err != nil {
			logger.Errorf("unable to remove temporary dockerfile %s: %s", tmpDockerFile.Name(), err)
		}
	}()

	// build the docker image
	err = dockerClient.Build(
		tmpDockerFile.Name(),
		s.buildContext.BaseDirectory,
		s.Name,
		s.buildContext.BuildArguments,
		strings.Split(s.buildContext.IgnoreFileContents, "\n"),
		logs,
	)
	if err != nil {
		return err
	}

	return nil
}

// FIXME: Duplicate code from service.go
func (b *Batch) RunContainer(stop <-chan bool, updates chan<- ServiceRunUpdate, opts ...RunContainerOption) error {
	runtimeOptions := lo.ToPtr(defaultRunContainerOptions)

	for _, opt := range opts {
		opt(runtimeOptions)
	}

	dockerClient, err := docker.New()
	if err != nil {
		return err
	}

	hostConfig := &container.HostConfig{
		// TODO: make this configurable through an cmd param
		AutoRemove: true,
		// LogConfig:  *f.ce.Logger(f.runCtx).Config(),
		LogConfig: container.LogConfig{
			Type: "json-file",
			Config: map[string]string{
				"max-size": "10m",
				"max-file": "3",
			},
		},
	}

	if goruntime.GOOS == "linux" {
		// setup host.docker.internal to route to host gateway
		// to access rpc server hosted by local CLI run
		hostConfig.ExtraHosts = []string{"host.docker.internal:172.17.0.1"}
	}

	randomPort, _ := netx.TakePort(1)
	hostProxyPort := fmt.Sprint(randomPort[0])
	env := []string{
		fmt.Sprintf("NITRIC_ENVIRONMENT=%s", runtimeOptions.nitricEnvironment),
		// FIXME: Ensure environment variable consistency in all SDKs, then remove duplicates here.
		fmt.Sprintf("SERVICE_ADDRESS=%s", fmt.Sprintf("%s:%s", runtimeOptions.nitricHost, runtimeOptions.nitricPort)),
		fmt.Sprintf("NITRIC_SERVICE_PORT=%s", runtimeOptions.nitricPort),
		fmt.Sprintf("NITRIC_SERVICE_HOST=%s", runtimeOptions.nitricHost),
		fmt.Sprintf("NITRIC_HTTP_PROXY_PORT=%d", randomPort[0]),
	}

	osEnv := os.Environ()

	// filter out env vars that can conflict with the container
	bannedVars := []string{"TEMP", "TMP", "PATH", "HOME"}

	osEnv = lo.Filter(osEnv, func(item string, index int) bool {
		return !lo.Contains(bannedVars, strings.Split(item, "=")[0])
	})

	env = append(env, osEnv...)

	for k, v := range runtimeOptions.envVars {
		env = append(env, k+"="+v)
	}

	hostConfig.PortBindings = nat.PortMap{
		nat.Port(hostProxyPort): []nat.PortBinding{
			{
				HostPort: hostProxyPort,
			},
		},
	}

	containerConfig := &container.Config{
		Image: b.Name, // Select an image to use based on the handler
		Env:   env,
		ExposedPorts: nat.PortSet{
			nat.Port(hostProxyPort): struct{}{},
		},
	}

	// Create the container
	containerId, err := dockerClient.ContainerCreate(
		containerConfig,
		hostConfig,
		nil,
		b.Name,
	)
	if err != nil {
		updates <- ServiceRunUpdate{
			ServiceName: b.Name,
			Status:      ServiceRunStatus_Error,
			Err:         err,
		}

		return nil
	}

	err = dockerClient.ContainerStart(context.TODO(), containerId, types.ContainerStartOptions{})
	if err != nil {
		updates <- ServiceRunUpdate{
			ServiceName: b.Name,
			Status:      ServiceRunStatus_Error,
			Err:         err,
		}

		return nil
	}

	updates <- ServiceRunUpdate{
		ServiceName: b.Name,
		Message:     fmt.Sprintf("Service %s started", b.Name),
		Status:      ServiceRunStatus_Running,
	}

	// Attach to the container to get stdout and stderr
	attachOptions := types.ContainerAttachOptions{
		Stream: true,
		Stdout: true,
		Stderr: true,
	}

	attachResponse, err := dockerClient.ContainerAttach(context.TODO(), containerId, attachOptions)
	if err != nil {
		return fmt.Errorf("error attaching to container %s: %w", b.Name, err)
	}

	// Use a separate goroutine to handle the container's output
	go func() {
		defer attachResponse.Close()
		// Using io.Copy to send the output to a writer
		_, err := io.Copy(writerFunc(func(p []byte) (int, error) {
			updates <- ServiceRunUpdate{
				ServiceName: b.Name,
				Message:     string(p),
				Status:      ServiceRunStatus_Running,
			}

			return len(p), nil
		}), attachResponse.Reader)
		if err != nil {
			updates <- ServiceRunUpdate{
				ServiceName: b.Name,
				Status:      ServiceRunStatus_Error,
				Err:         err,
			}
		}
	}()

	okChan, errChan := dockerClient.ContainerWait(context.TODO(), containerId, container.WaitConditionNotRunning)

	for {
		select {
		case err := <-errChan:
			updates <- ServiceRunUpdate{
				ServiceName: b.Name,
				Err:         err,
				Status:      ServiceRunStatus_Error,
			}

			return err
		case okBody := <-okChan:
			if okBody.StatusCode != 0 {
				logOptions := types.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Tail: "20"}

				logReader, err := dockerClient.ContainerLogs(context.Background(), containerId, logOptions)
				if err != nil {
					return err
				}

				// Create a buffer to hold the logs
				var logs bytes.Buffer
				if _, err := stdcopy.StdCopy(&logs, &logs, logReader); err != nil {
					return fmt.Errorf("error reading logs for service %s: %w", b.Name, err)
				}

				err = fmt.Errorf("service %s exited with non 0 status\n %s", b.Name, logs.String())

				updates <- ServiceRunUpdate{
					ServiceName: b.Name,
					// TODO: Extract the error logs for the container here...
					Err:    err,
					Status: ServiceRunStatus_Error,
				}

				return err
			} else {
				updates <- ServiceRunUpdate{
					ServiceName: b.Name,
					Message:     "Service successfully exited",
					Status:      ServiceRunStatus_Done,
				}
			}

			return nil
		case <-stop:
			if err := dockerClient.ContainerStop(context.Background(), containerId, nil); err != nil {
				updates <- ServiceRunUpdate{
					ServiceName: b.Name,
					Status:      ServiceRunStatus_Error,
					Err:         err,
				}

				return nil
			}
		}
	}
}
