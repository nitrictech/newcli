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

package run

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/pkg/errors"
	"github.com/pterm/pterm"

	"github.com/nitrictech/cli/pkg/containerengine"
	"github.com/nitrictech/cli/pkg/utils"
)

type MinioServer struct {
	dir     string
	name    string
	cid     string
	ce      containerengine.ContainerEngine
	apiPort int // external API port from the minio container
}

const (
	minioImage       = "minio/minio:latest"
	devVolume        = "/nitric/"
	runPerm          = os.ModePerm // NOTE: octal notation is important here!!!
	labelStackName   = "io.nitric/stack"
	labelType        = "io.nitric/type"
	minioPort        = 9000 // internal minio api port
	minioConsolePort = 9001 // internal minio console port
)

// Start - Start the local Minio server
func (m *MinioServer) Start() error {
	runDir, err := filepath.Abs(m.dir)
	if err != nil {
		return err
	}

	err = os.MkdirAll(runDir, runPerm)
	if err != nil {
		return errors.WithMessage(err, "os.MkdirAll")
	}

	ports, err := utils.Take(2)
	if err != nil {
		return errors.WithMessage(err, "freeport.Take")
	}

	port := uint16(ports[0])
	consolePort := uint16(ports[1])

	err = m.ce.ImagePull(minioImage, types.ImagePullOptions{})
	if err != nil {
		return err
	}

	cc := &container.Config{
		Image: minioImage,
		Cmd:   []string{"minio", "server", "/nitric/buckets", "--console-address", fmt.Sprintf(":%d", minioConsolePort)},
		ExposedPorts: nat.PortSet{
			nat.Port(fmt.Sprintf("%d/tcp", minioPort)):        struct{}{},
			nat.Port(fmt.Sprintf("%d/tcp", minioConsolePort)): struct{}{},
		},
		Labels: map[string]string{
			labelStackName: m.name,
			labelType:      "minio",
		},
	}

	hc := &container.HostConfig{
		AutoRemove: true,
		PortBindings: nat.PortMap{
			nat.Port(fmt.Sprintf("%d/tcp", minioPort)): []nat.PortBinding{
				{
					HostPort: fmt.Sprintf("%d", port),
				},
			},
			nat.Port(fmt.Sprintf("%d/tcp", minioConsolePort)): []nat.PortBinding{
				{
					HostPort: fmt.Sprintf("%d", consolePort),
				},
			},
		},
		Mounts: []mount.Mount{
			{
				Source: runDir,
				Type:   mount.TypeBind,
				Target: devVolume,
			},
		},
		LogConfig:   *m.ce.Logger(m.dir).Config(),
		NetworkMode: container.NetworkMode("bridge"),
	}

	cID, err := m.ce.ContainerCreate(cc, hc, &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{},
	}, "minio-"+m.name)
	if err != nil {
		return err
	}

	m.cid = cID
	m.apiPort = int(port)

	pterm.Debug.Print(containerengine.Cli(cc, hc))

	return m.ce.Start(cID)
}

func (m *MinioServer) GetApiPort() int {
	return m.apiPort
}

func (m *MinioServer) Stop() error {
	timeout := time.Second * 5

	return m.ce.Stop(m.cid, &timeout)
}

func NewMinio(dir string, name string) (*MinioServer, error) {
	ce, err := containerengine.Discover()
	if err != nil {
		return nil, err
	}

	// Remove any existing containers with this label.
	err = ce.RemoveByLabel(map[string]string{
		labelStackName: name,
		labelType:      "minio",
	})
	if err != nil {
		return nil, errors.WithMessage(err, "could not remove existing minio container")
	}

	return &MinioServer{
		ce:   ce,
		dir:  dir,
		name: name,
	}, nil
}
