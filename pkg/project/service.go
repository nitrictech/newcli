// Copyright Nitric Pty Ltd.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package project

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"syscall"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/samber/lo"
	"github.com/spf13/afero"

	"github.com/nitrictech/cli/pkg/docker"
	"github.com/nitrictech/cli/pkg/netx"
	"github.com/nitrictech/cli/pkg/project/runtime"
	"github.com/nitrictech/nitric/core/pkg/logger"
)

type ServiceBuildStatus string

type Service struct {
	Name string
	Type string

	// filepath relative to the project root directory
	filepath     string
	buildContext runtime.RuntimeBuildContext

	startCmd string
}

func (s *Service) GetFilePath() string {
	return s.filepath
}

func (s *Service) GetAbsoluteFilePath() (string, error) {
	return filepath.Abs(s.filepath)
}

const (
	ServiceBuildStatus_InProgress ServiceBuildStatus = "In Progress"
	ServiceBuildStatus_Complete   ServiceBuildStatus = "Complete"
	ServiceBuildStatus_Error      ServiceBuildStatus = "Error"
	ServiceBuildStatus_Skipped    ServiceBuildStatus = "Skipped"
)

type ServiceBuildUpdate struct {
	ServiceName string
	Message     string
	Status      ServiceBuildStatus
	Err         error
}

type ServiceRunStatus string

const (
	ServiceRunStatus_Running ServiceRunStatus = "Running"
	ServiceRunStatus_Done    ServiceRunStatus = "Done"
	ServiceRunStatus_Error   ServiceRunStatus = "Error"
)

type ServiceRunUpdate struct {
	ServiceName string
	Label       string
	Message     string
	Status      ServiceRunStatus
	Err         error
}

type ServiceRunUpdateWriter struct {
	updates     chan<- ServiceRunUpdate
	serviceName string
	label       string
	status      ServiceRunStatus
}

func (s *ServiceRunUpdateWriter) Write(data []byte) (int, error) {
	msg := string(data)

	s.updates <- ServiceRunUpdate{
		ServiceName: s.serviceName,
		Message:     msg,
		Status:      s.status,
		Label:       s.label,
	}

	return len(data), nil
}

type serviceBuildUpdateWriter struct {
	serviceName     string
	buildUpdateChan chan ServiceBuildUpdate
}

func (b *serviceBuildUpdateWriter) Write(data []byte) (int, error) {
	b.buildUpdateChan <- ServiceBuildUpdate{
		ServiceName: b.serviceName,
		Message:     string(data),
		Status:      ServiceBuildStatus_InProgress,
	}

	return len(data), nil
}

func (s *Service) BuildImage(fs afero.Fs, logs io.Writer) error {
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

type runContainerOptions struct {
	nitricHost        string
	nitricPort        string
	nitricEnvironment string
	envVars           map[string]string
}

type RunContainerOption func(*runContainerOptions)

var defaultRunContainerOptions = runContainerOptions{
	nitricHost:        "host.docker.internal",
	nitricPort:        "50051",
	nitricEnvironment: "run",
	envVars:           map[string]string{},
}

func WithNitricHost(host string) RunContainerOption {
	return func(o *runContainerOptions) {
		o.nitricHost = host
	}
}

func WithNitricPort(port string) RunContainerOption {
	return func(o *runContainerOptions) {
		o.nitricPort = port
	}
}

func WithNitricEnvironment(environment string) RunContainerOption {
	return func(o *runContainerOptions) {
		o.nitricEnvironment = environment
	}
}

func WithEnvVars(envVars map[string]string) RunContainerOption {
	return func(o *runContainerOptions) {
		o.envVars = envVars
	}
}

type writerFunc func(p []byte) (n int, err error)

func (wf writerFunc) Write(p []byte) (n int, err error) {
	return wf(p)
}

// Run - runs the service using the provided command, typically not in a container.
func (s *Service) Run(stop <-chan bool, updates chan<- ServiceRunUpdate, env map[string]string) error {
	if s.startCmd == "" {
		return fmt.Errorf("no start command provided for service %s", s.filepath)
	}

	// this could be improve with real env var substitution.
	startCmd := strings.ReplaceAll(s.startCmd, "$SERVICE_PATH", s.filepath)
	startCmd = strings.ReplaceAll(startCmd, "${SERVICE_PATH}", s.filepath)

	if !strings.Contains(startCmd, s.filepath) {
		logger.Warnf("Start cmd for service %s does not contain $SERVICE_PATH, check the service start configuration in nitric.yaml", s.filepath)
	}

	commandParts := strings.Split(startCmd, " ")
	cmd := exec.Command(
		commandParts[0],
		commandParts[1:]...,
	)

	cmd.Env = append(cmd.Env, os.Environ()...)
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	err := os.MkdirAll(".nitric/logs", os.ModePerm)
	if err != nil {
		return err
	}

	// Create a file writer for the logs
	logFile, err := os.Create(filepath.Join(".nitric/logs/", fmt.Sprintf("%s.log", s.Name)))
	if err != nil {
		return err
	}

	errorLogFile, err := os.Create(filepath.Join(".nitric/logs/", fmt.Sprintf("%s.error.log", s.Name)))
	if err != nil {
		return err
	}

	// cmd.Env = append(cmd.Env, fmt.Sprintf("SERVICE_PATH=\"%s\"", s.filepath))

	cmd.Stdout = io.MultiWriter(&ServiceRunUpdateWriter{
		updates:     updates,
		serviceName: s.Name,
		label:       s.filepath,
		status:      ServiceRunStatus_Running,
	}, logFile)

	cmd.Stderr = io.MultiWriter(&ServiceRunUpdateWriter{
		updates:     updates,
		serviceName: s.Name,
		label:       s.filepath,
		status:      ServiceRunStatus_Error,
	}, errorLogFile)

	errChan := make(chan error)

	go func() {
		err := cmd.Start()
		if err != nil {
			errChan <- err
		} else {
			updates <- ServiceRunUpdate{
				ServiceName: s.Name,
				Label:       "nitric",
				Status:      ServiceRunStatus_Running,
				Message:     fmt.Sprintf("started service %s", s.filepath),
			}
		}

		err = cmd.Wait()
		errChan <- err
	}()

	go func(cmd *exec.Cmd) {
		<-stop

		err := cmd.Process.Signal(syscall.SIGTERM)
		if err != nil {
			_ = cmd.Process.Kill()
		}
	}(cmd)

	err = <-errChan
	updates <- ServiceRunUpdate{
		ServiceName: s.Name,
		Status:      ServiceRunStatus_Error,
		Err:         err,
	}

	return err
}

// RunContainer - Runs a container for the service, blocking until the container exits
func (s *Service) RunContainer(stop <-chan bool, updates chan<- ServiceRunUpdate, opts ...RunContainerOption) error {
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
		Image: s.Name, // Select an image to use based on the handler
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
		s.Name,
	)
	if err != nil {
		updates <- ServiceRunUpdate{
			ServiceName: s.Name,
			Status:      ServiceRunStatus_Error,
			Err:         err,
		}

		return nil
	}

	err = dockerClient.ContainerStart(context.TODO(), containerId, types.ContainerStartOptions{})
	if err != nil {
		updates <- ServiceRunUpdate{
			ServiceName: s.Name,
			Status:      ServiceRunStatus_Error,
			Err:         err,
		}

		return nil
	}

	updates <- ServiceRunUpdate{
		ServiceName: s.Name,
		Message:     fmt.Sprintf("Service %s started", s.Name),
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
		return fmt.Errorf("error attaching to container %s: %w", s.Name, err)
	}

	// Use a separate goroutine to handle the container's output
	go func() {
		defer attachResponse.Close()
		// Using io.Copy to send the output to a writer
		_, err := io.Copy(writerFunc(func(p []byte) (int, error) {
			updates <- ServiceRunUpdate{
				ServiceName: s.Name,
				Message:     string(p),
				Status:      ServiceRunStatus_Running,
			}

			return len(p), nil
		}), attachResponse.Reader)
		if err != nil {
			updates <- ServiceRunUpdate{
				ServiceName: s.Name,
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
				ServiceName: s.Name,
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
					return fmt.Errorf("error reading logs for service %s: %w", s.Name, err)
				}

				err = fmt.Errorf("service %s exited with non 0 status\n %s", s.Name, logs.String())

				updates <- ServiceRunUpdate{
					ServiceName: s.Name,
					// TODO: Extract the error logs for the container here...
					Err:    err,
					Status: ServiceRunStatus_Error,
				}

				return err
			} else {
				updates <- ServiceRunUpdate{
					ServiceName: s.Name,
					Message:     "Service successfully exited",
					Status:      ServiceRunStatus_Done,
				}
			}

			return nil
		case <-stop:
			if err := dockerClient.ContainerStop(context.Background(), containerId, nil); err != nil {
				updates <- ServiceRunUpdate{
					ServiceName: s.Name,
					Status:      ServiceRunStatus_Error,
					Err:         err,
				}

				return nil
			}
		}
	}
}
