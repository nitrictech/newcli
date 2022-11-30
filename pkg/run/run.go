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
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/utils"
	"github.com/nitrictech/nitric/pkg/membrane"
	boltdb_service "github.com/nitrictech/nitric/pkg/plugins/document/boltdb"
	queue_service "github.com/nitrictech/nitric/pkg/plugins/queue/dev"
	secret_service "github.com/nitrictech/nitric/pkg/plugins/secret/dev"
	nitric_utils "github.com/nitrictech/nitric/pkg/utils"
	"github.com/nitrictech/nitric/pkg/worker"
)

type LocalServices interface {
	Start(pool worker.WorkerPool, statusChannel chan LocalServicesStatus) error
	Stop() error
	Running() bool
	Status() *LocalServicesStatus
}

type ServiceStatus int

const (
	Stopped ServiceStatus = iota
	Started
	Starting
)

type LocalServicesStatus struct {
	RunDir          string        `yaml:"runDir"`
	GatewayAddress  string        `yaml:"gatewayAddress"`
	MembraneAddress string        `yaml:"membraneAddress"`
	StorageEndpoint string        `yaml:"storageEndpoint"`
	StorageStatus   ServiceStatus `yaml:"storageStatus"`
}

type localServices struct {
	s             *project.Project
	storage       *SeaweedServer
	mem           *membrane.Membrane
	status        *LocalServicesStatus
	statusChannel chan LocalServicesStatus
}

func NewLocalServices(s *project.Project, statusChannel chan LocalServicesStatus) LocalServices {
	return &localServices{
		s:             s,
		statusChannel: statusChannel,
		status: &LocalServicesStatus{
			RunDir:          filepath.Join(utils.NitricRunDir(), s.Name),
			GatewayAddress:  nitric_utils.GetEnv("GATEWAY_ADDRESS", ":9001"),
			MembraneAddress: net.JoinHostPort("localhost", "50051"),
		},
	}
}

func (l *localServices) Stop() error {
	l.mem.Stop()
	return l.storage.Stop()
}

func (l *localServices) Running() bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("0.0.0.0", "50051"), time.Second)
	if err == nil && conn != nil {
		defer conn.Close()

		return true
	}

	return false
}

func (l *localServices) Status() *LocalServicesStatus {
	return l.status
}

func (l *localServices) Start(pool worker.WorkerPool, statusChannel chan LocalServicesStatus) error {
	var err error

	l.storage, err = NewSeaweed(l.status.RunDir)
	if err != nil {
		return err
	}

	// start seaweed server
	err = l.storage.Start()
	if err != nil {
		return err
	}

	go func() {
		l.status.StorageStatus = Starting
		if statusChannel != nil {
			statusChannel <- *l.status
		}
	}()

	storageAddr := fmt.Sprintf("localhost:%d", l.storage.GetApiPort())
	l.status.StorageEndpoint = fmt.Sprintf("http://%s", storageAddr)

	go func() {
		var conn net.Conn = nil
		for attempts := 0; conn == nil || attempts < 60; attempts++ {
			conn, err = net.DialTimeout("tcp", storageAddr, time.Second)
		}

		if err != nil {
			println(err.Error())
			l.status.StorageStatus = Stopped
		}
		if conn != nil {
			defer conn.Close()
			l.status.StorageStatus = Started
		}
		if statusChannel != nil {
			statusChannel <- *l.status
		}
	}()

	sp, err := NewStorage(StorageOptions{
		AccessKey: "dummykey",
		SecretKey: "dummysecret",
		Endpoint:  l.status.StorageEndpoint,
	})
	if err != nil {
		return err
	}

	// Connect dev documents
	os.Setenv("LOCAL_DB_DIR", l.status.RunDir)

	dp, err := boltdb_service.New()
	if err != nil {
		return err
	}

	// Connect secrets plugin
	os.Setenv("LOCAL_SEC_DIR", l.status.RunDir)

	secp, err := secret_service.New()
	if err != nil {
		return err
	}

	// Connect queue plugin
	os.Setenv("LOCAL_QUEUE_DIR", l.status.RunDir)

	qp, err := queue_service.New()
	if err != nil {
		return err
	}

	ev, err := NewEvents(pool)
	if err != nil {
		return err
	}

	// create new resources
	res := NewResources(fmt.Sprintf("http://localhost%s", l.status.GatewayAddress))

	// Start a new gateway plugin
	gw, err := NewGateway(l.status.GatewayAddress)
	if err != nil {
		return err
	}

	// Prepare development membrane to start
	// This will start a single membrane that all
	// running functions will connect to
	l.mem, err = membrane.New(&membrane.MembraneOptions{
		ServiceAddress:          "0.0.0.0:50051",
		SecretPlugin:            secp,
		QueuePlugin:             qp,
		StoragePlugin:           sp,
		DocumentPlugin:          dp,
		GatewayPlugin:           gw,
		EventsPlugin:            ev,
		ResourcesPlugin:         res,
		Pool:                    pool,
		TolerateMissingServices: false,
	})
	if err != nil {
		return err
	}

	return l.mem.Start()
}
