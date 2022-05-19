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
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/docker/cli/cli/command/image/build"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/idtools"
	"github.com/pkg/errors"
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
		fmt.Println("docker daemon not running, please start it..")
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

func tarContextDir(relDockerfile, contextDir string, extraExcludes []string) (io.ReadCloser, error) {
	excludes, err := build.ReadDockerignore(contextDir)
	if err != nil {
		return nil, err
	}

	excludes = append(excludes, utils.NitricLogDir(contextDir))
	excludes = append(excludes, extraExcludes...)

	if err := build.ValidateContextDirectory(contextDir, excludes); err != nil {
		return nil, errors.Errorf("error checking context: '%s'.", err)
	}

	excludes = build.TrimBuildFilesFromExcludes(excludes, relDockerfile, false)
	return archive.TarWithOptions(contextDir, &archive.TarOptions{
		ExcludePatterns: excludes,
		ChownOpts:       &idtools.Identity{UID: 0, GID: 0},
	})
}

func imageNameFromBuildContext(dockerfile, srcPath, imageTag string, excludes []string) (string, error) {
	var buildContext io.ReadCloser
	var err error
	if strings.Contains(dockerfile, "nitric.dynamic.") {
		// don't include the dynamic dockerfile as the timestamp on the file will cause it to have a different hash.
		buildContext, err = tarContextDir("", srcPath, append(excludes, dockerfile))
	} else {
		buildContext, err = tarContextDir(dockerfile, srcPath, excludes)
	}
	if err != nil {
		return "", err
	}

	hash := md5.New()
	_, err = io.Copy(hash, buildContext)
	if err != nil {
		return "", err
	}

	imageName := imageTag
	if strings.Contains(imageTag, ":") {
		imageName = strings.Split(imageTag, ":")[0]
	}

	return strings.ToLower(imageName + ":" + hex.EncodeToString(hash.Sum(nil))), nil
}

func (d *docker) Build(dockerfile, srcPath, imageTag string, buildArgs map[string]string, excludes []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout())
	defer cancel()

	imageTagWithHash, err := imageNameFromBuildContext(dockerfile, srcPath, imageTag, excludes)
	if err != nil {
		return err
	}

	buildContext, err := tarContextDir(dockerfile, srcPath, excludes)
	if err != nil {
		return err
	}

	// try and find an existing image with this hash.
	listOpts := types.ImageListOptions{Filters: filters.NewArgs()}
	listOpts.Filters.Add("reference", imageTagWithHash)
	imageSummaries, err := d.cli.ImageList(ctx, listOpts)
	if err == nil && len(imageSummaries) > 0 {
		return nil
	}

	opts := types.ImageBuildOptions{
		SuppressOutput: false,
		Dockerfile:     dockerfile,
		Platform:       "linux/amd64",
		Tags:           []string{strings.ToLower(imageTag), imageTagWithHash},
		Remove:         true,
		ForceRemove:    true,
		PullParent:     true,
	}
	res, err := d.cli.ImageBuild(ctx, buildContext, opts)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	return print(res.Body)
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
		if len(strings.TrimSpace(line.Stream)) > 0 {
			if strings.Contains(line.Stream, "--->") {
				if output.VerboseLevel >= 3 {
					log.Default().Print(line.Stream)
				}
			} else {
				log.Default().Print(line.Stream)
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
