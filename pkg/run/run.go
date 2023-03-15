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
	"github.com/nitrictech/nitric/core/pkg/membrane"
	"github.com/nitrictech/nitric/core/pkg/worker/pool"
)

type LocalServices interface {
	Start(pool pool.WorkerPool) error
	Stop() error
	Running() bool
	Status() *LocalServicesStatus
	Refresh() error
	Apis() map[string]string
	TriggerAddress() string
}

type LocalServicesStatus struct {
	RunDir string `yaml:"runDir"`
	// GatewayAddress  string `yaml:"gatewayAddress"`
	MembraneAddress string `yaml:"membraneAddress"`
	StorageEndpoint string `yaml:"storageEndpoint"`
}

type localServices struct {
	s       *project.Project
	storage *SeaweedServer
	mem     *membrane.Membrane
	status  *LocalServicesStatus
	gw      *BaseHttpGateway
	isStart bool
}

func NewLocalServices(s *project.Project, isStart bool) LocalServices {
	return &localServices{
		s:       s,
		isStart: isStart,
		status: &LocalServicesStatus{
			RunDir:          filepath.Join(utils.NitricRunDir(), s.Name),
			MembraneAddress: net.JoinHostPort("localhost", "50051"),
		},
	}
}

func (l *localServices) TriggerAddress() string {
	if l.gw != nil {
		return l.gw.GetTriggerAddress()
	}

	return ""
}

func (l *localServices) Refresh() error {
	if l.gw != nil {
		return l.gw.Refresh()
	}

	return nil
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

func (l *localServices) Apis() map[string]string {
	if l.gw != nil {
		return l.gw.GetApiAddresses()
	}

	return nil
}

func (l *localServices) Status() *LocalServicesStatus {
	return l.status
}

func (l *localServices) Start(pool pool.WorkerPool) error {
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

	l.status.StorageEndpoint = fmt.Sprintf("http://localhost:%d", l.storage.GetApiPort())

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

	dp, err := NewBoltService()
	if err != nil {
		return err
	}

	// Connect secrets plugin
	os.Setenv("LOCAL_SEC_DIR", l.status.RunDir)

	secp, err := NewSecretService()
	if err != nil {
		return err
	}

	// Connect queue plugin
	os.Setenv("LOCAL_QUEUE_DIR", l.status.RunDir)

	qp, err := NewQueueService()
	if err != nil {
		return err
	}

	ev, err := NewEvents(pool)
	if err != nil {
		return err
	}

	// Start a new gateway plugin
	l.gw, err = NewGateway()
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
		GatewayPlugin:           l.gw,
		EventsPlugin:            ev,
		ResourcesPlugin:         NewResources(l.gw, l.isStart),
		Pool:                    pool,
		TolerateMissingServices: false,
	})
	if err != nil {
		return err
	}

	return l.mem.Start()
}
