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
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	cliconfig "github.com/docker/cli/cli/config"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/session/filesync"
	"github.com/pkg/errors"
	fsutiltypes "github.com/tonistiigi/fsutil/types"
)

type docker struct {
	cli *client.Client
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

func tryNodeIdentifier() string {
	out := cliconfig.Dir() // return config dir as default on permission error
	if err := os.MkdirAll(cliconfig.Dir(), 0700); err == nil {
		sessionFile := filepath.Join(cliconfig.Dir(), ".buildNodeID")
		if _, err := os.Lstat(sessionFile); err != nil {
			if os.IsNotExist(err) { // create a new file with stored randomness
				b := make([]byte, 32)
				if _, err := rand.Read(b); err != nil {
					return out
				}
				if err := ioutil.WriteFile(sessionFile, []byte(hex.EncodeToString(b)), 0600); err != nil {
					return out
				}
			}
		}

		dt, err := ioutil.ReadFile(sessionFile)
		if err == nil {
			return string(dt)
		}
	}
	return out
}

func getBuildSharedKey(dir string) string {
	s := sha256.Sum256([]byte(fmt.Sprintf("%s:%s", tryNodeIdentifier(), dir)))
	return hex.EncodeToString(s[:])
}

func resetUIDAndGID(_ string, s *fsutiltypes.Stat) bool {
	s.Uid = 0
	s.Gid = 0
	return true
}

func (d *docker) Build(dockerfile, srcPath, imageTag string, buildArgs map[string]string) error {
	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout())
	defer cancel()

	s, err := session.NewSession(context.TODO(), filepath.Base(srcPath), getBuildSharedKey(srcPath))
	if err != nil {
		return err
	}

	dockerfileName := filepath.Base(dockerfile)
	dockerfileDir := filepath.Dir(dockerfile)

	s.Allow(filesync.NewFSSyncProvider([]filesync.SyncedDir{
		{
			Name: "context",
			Dir:  srcPath,
			Map:  resetUIDAndGID,
		},
		{
			Name: "dockerfile",
			Dir:  dockerfileDir,
		},
	}))

	dockerAuthProvider := authprovider.NewDockerAuthProvider(os.Stderr)
	s.Allow(dockerAuthProvider)

	dialSession := func(ctx context.Context, proto string, meta map[string][]string) (net.Conn, error) {
		return d.cli.DialHijack(ctx, "/session", proto, meta)
	}
	go (func() error {
		return s.Run(context.TODO(), dialSession)
	})()

	opts := types.ImageBuildOptions{
		Version:        types.BuilderBuildKit,
		SuppressOutput: false,
		Dockerfile:     dockerfileName,
		Tags:           []string{imageTag},
		Remove:         true,
		RemoteContext:  "client-session",
		ForceRemove:    true,
		PullParent:     true,
		SessionID:      s.ID(),
		Outputs:        []types.ImageBuildOutput{},
	}
	res, err := d.cli.ImageBuild(ctx, nil, opts)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	//displayCh := make(chan *bClient.SolveStatus)

	//displayStatus := func(out *os.File, displayCh chan *bClient.SolveStatus) {
	//	var c console.Console
	//	// TODO: Handle tty output in non-tty environment.
	//	if cons, err := console.ConsoleFromFile(out); err == nil {
	//		c = cons
	//	}
	//	// not using shared context to not disrupt display but let it finish reporting errors

	//	go func() error {
	//		return progressui.DisplaySolveStatus(context.TODO(), "", c, out, displayCh)
	//	}()
	//}

	//displayStatus(os.Stderr, displayCh)

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
		json.Unmarshal([]byte(lastLine), line)
		if len(line.Stream) > 0 {
			fmt.Print(line.Stream)
		}
	}

	errLine := &ErrorLine{}
	json.Unmarshal([]byte(lastLine), errLine)
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

func (d *docker) ImagePull(rawImage string) error {
	resp, err := d.cli.ImagePull(context.Background(), rawImage, types.ImagePullOptions{})
	if err != nil {
		return errors.WithMessage(err, "Pull")
	}
	defer resp.Close()
	print(resp)
	return nil
}

func (d *docker) NetworkCreate(name string) error {
	_, err := d.cli.NetworkInspect(context.Background(), name, types.NetworkInspectOptions{})
	if err == nil {
		// it already exists, no need to create.
		return nil
	}
	_, err = d.cli.NetworkCreate(context.Background(), name, types.NetworkCreate{})
	return err
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

func (d *docker) CopyFromArchive(nameOrID string, path string, reader io.Reader) error {
	return d.cli.CopyToContainer(context.Background(), nameOrID, path, reader, types.CopyToContainerOptions{})
}

func (d *docker) ContainersListByLabel(match map[string]string) ([]types.Container, error) {
	opts := types.ContainerListOptions{
		All:     true,
		Filters: filters.NewArgs(),
	}
	for k, v := range match {
		opts.Filters.Add("label", fmt.Sprintf("%s=%s", k, v))
	}

	return d.cli.ContainerList(context.Background(), opts)
}

func (d *docker) RemoveByLabel(name, value string) error {
	opts := types.ContainerListOptions{
		All:     true,
		Filters: filters.NewArgs(),
	}
	opts.Filters.Add("label", fmt.Sprintf("%s=%s", name, value))

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

func (d *docker) ContainerExec(containerName string, cmd []string, workingDir string) error {
	ctx := context.Background()
	rst, err := d.cli.ContainerExecCreate(ctx, containerName, types.ExecConfig{
		WorkingDir: workingDir,
		Cmd:        cmd,
	})
	if err != nil {
		return err
	}
	err = d.cli.ContainerExecStart(ctx, rst.ID, types.ExecStartCheck{})
	if err != nil {
		return err
	}

	for {
		res, err := d.cli.ContainerExecInspect(ctx, rst.ID)
		if err != nil {
			return err
		}
		if res.Running {
			continue
		}
		if res.ExitCode == 0 {
			return nil
		}
		return fmt.Errorf("%s %v exited with %d", containerName, cmd, res.ExitCode)
	}
}
