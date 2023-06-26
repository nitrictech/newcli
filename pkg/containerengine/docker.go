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

package containerengine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
	"github.com/pterm/pterm"
	"gopkg.in/yaml.v2"

	"github.com/nitrictech/cli/pkg/output"
	"github.com/nitrictech/cli/pkg/utils"
)

type docker struct {
	cli    *client.Client
	logger ContainerLogger
}

var _ ContainerEngine = &docker{}

func newDocker() (ContainerEngine, error) {
	cmd := exec.Command("docker", "--version")

	err := cmd.Run()
	if err != nil {
		return nil, err
	}

	cmd = exec.Command("docker", "ps")

	err = cmd.Run()
	if err != nil {
		return nil, err
	}

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return nil, err
	}

	return &docker{cli: cli}, err
}

func (d *docker) Type() string {
	return "docker"
}

func (d *docker) Inspect(imageName string) (types.ImageInspect, error) {
	ii, _, err := d.cli.ImageInspectWithRaw(context.Background(), imageName)

	return ii, err
}

// Create a known nitric container builder to allow custom cache configuration
func (d *docker) createBuider() error {
	// Create a known fixed nitric builder to allow caching
	cmd := exec.Command("docker", "buildx", "create", "--name", "nitric", "--driver=docker-container", "--node", "nitric0")
	return cmd.Run()
}

func (d *docker) Build(dockerfile, srcPath, imageTag string, buildArgs map[string]string, excludes []string) error {
	err := d.createBuider()
	if err != nil {
		return err
	}
	// write a temporary dockerignore file
	ignoreFile, err := os.Create(fmt.Sprintf("%s.dockerignore", dockerfile))
	if err != nil {
		return err
	}

	_, err = ignoreFile.Write([]byte(strings.Join(excludes, "\n")))
	if err != nil {
		return err
	}

	err = ignoreFile.Close()
	if err != nil {
		return err
	}

	defer func() {
		os.Remove(ignoreFile.Name())
	}()

	buildArgsCmd := make([]string, 0)
	for k, v := range buildArgs {
		buildArgsCmd = append(buildArgsCmd, "--build-arg", fmt.Sprintf("%s=%s", k, v))
	}

	args := []string{
		"buildx", "build", srcPath, "-f", dockerfile, "-t", imageTag, "--load", "--builder=nitric", "--platform", "linux/amd64",
	}
	args = append(args, buildArgsCmd...)

	dockerBuildCache := os.Getenv("DOCKER_BUILD_CACHE")
	if dockerBuildCache != "" {
		imageCache := filepath.Join(dockerBuildCache, imageTag)

		cacheTo := fmt.Sprintf("--cache-to=type=local,dest=%s", imageCache)
		cacheFrom := fmt.Sprintf("--cache-from=type=local,src=%s", imageCache)

		args = append(args, cacheTo, cacheFrom)
	}

	cmd := exec.Command("docker", args...)
	lb := utils.NewLineBuffer(5)

	cmd.Stderr = io.MultiWriter(lb, output.NewPtermWriter(pterm.Debug))
	cmd.Stdout = output.NewPtermWriter(pterm.Debug)

	pterm.Debug.Println("running command: " + cmd.String())

	err = cmd.Run()

	if err != nil {
		return fmt.Errorf("%s", strings.Join(lb.Lines, "\n"))
	}

	return nil
}

type ErrorLine struct {
	Error       string      `json:"error"`
	ErrorDetail ErrorDetail `json:"errorDetail"`
}

type ErrorDetail struct {
	Message string `json:"message"`
}

type Line struct {
	Stream string `json:"stream"`
	Status string `json:"status"`
}

func print(rd io.Reader) error {
	var lastLine string

	scanner := bufio.NewScanner(rd)
	for scanner.Scan() {
		lastLine = scanner.Text()
		line := &Line{}

		err := json.Unmarshal([]byte(lastLine), line)
		if err != nil {
			return err
		}

		text := strings.TrimRightFunc(line.Stream, unicode.IsSpace)

		if len(text) > 0 {
			if strings.Contains(text, "--->") {
				if output.VerboseLevel >= 3 {
					log.Default().Println(text)
				}
			} else {
				log.Default().Println(text)
			}
		}
	}

	errLine := &ErrorLine{}

	err := json.Unmarshal([]byte(lastLine), errLine)
	if err != nil {
		return err
	}

	if errLine.Error != "" {
		return errors.New(errLine.Error)
	}

	return scanner.Err()
}

func (d *docker) ListImages(stackName, containerName string) ([]Image, error) {
	opts := types.ImageListOptions{Filters: filters.NewArgs()}
	opts.Filters.Add("reference", fmt.Sprintf("%s-%s-*", stackName, containerName))

	imageSummaries, err := d.cli.ImageList(context.Background(), opts)
	if err != nil {
		return nil, err
	}

	imgs := []Image{}

	for _, i := range imageSummaries {
		nameParts := strings.Split(i.RepoTags[0], ":")
		id := strings.Split(i.ID, ":")[1][0:12]

		imgs = append(imgs, Image{
			ID:         id,
			Repository: nameParts[0],
			Tag:        nameParts[1],
			CreatedAt:  time.Unix(i.Created, 0).Local().String(),
		})
	}

	return imgs, err
}

func (d *docker) ImagePull(rawImage string, opts types.ImagePullOptions) error {
	resp, err := d.cli.ImagePull(context.Background(), rawImage, opts)
	if err != nil {
		return errors.WithMessage(err, "Pull")
	}

	defer resp.Close()

	return print(resp)
}

func (d *docker) ContainerCreate(config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, name string) (string, error) {
	resp, err := d.cli.ContainerCreate(context.Background(), config, hostConfig, networkingConfig, nil, name)
	if err != nil {
		return "", errors.WithMessage(err, "ContainerCreate")
	}

	return resp.ID, nil
}

func (d *docker) Start(nameOrID string) error {
	return d.cli.ContainerStart(context.Background(), nameOrID, types.ContainerStartOptions{})
}

func (d *docker) Stop(nameOrID string, timeout *time.Duration) error {
	return d.cli.ContainerStop(context.Background(), nameOrID, timeout)
}

func (d *docker) RemoveByLabel(labels map[string]string) error {
	opts := types.ContainerListOptions{
		All:     true,
		Filters: filters.NewArgs(),
	}

	for name, value := range labels {
		opts.Filters.Add("label", fmt.Sprintf("%s=%s", name, value))
	}

	res, err := d.cli.ContainerList(context.Background(), opts)
	if err != nil {
		return err
	}

	for _, con := range res {
		err = d.cli.ContainerRemove(context.Background(), con.ID, types.ContainerRemoveOptions{Force: true})
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *docker) ContainerWait(containerID string, condition container.WaitCondition) (<-chan container.ContainerWaitOKBody, <-chan error) {
	return d.cli.ContainerWait(context.Background(), containerID, condition)
}

func (d *docker) ContainerLogs(containerID string, opts types.ContainerLogsOptions) (io.ReadCloser, error) {
	return d.cli.ContainerLogs(context.Background(), containerID, opts)
}

func (d *docker) Logger(stackPath string) ContainerLogger {
	if d.logger != nil {
		return d.logger
	}

	logPath, _ := utils.NewNitricLogFile(stackPath)
	d.logger = newSyslog(logPath)

	return d.logger
}

func (d *docker) Version() string {
	sv, _ := d.cli.ServerVersion(context.Background())
	b, _ := yaml.Marshal(sv)

	return string(b)
}
