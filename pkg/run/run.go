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

	"github.com/nitrictech/cli/pkg/dashboard"
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
	HttpWorkers() map[int]string
	TriggerAddress() string
	GetWorkerPool() pool.WorkerPool
	GetDashPort() *int
}

type LocalServicesStatus struct {
	RunDir string `yaml:"runDir"`
	// GatewayAddress  string `yaml:"gatewayAddress"`
	MembraneAddress string `yaml:"membraneAddress"`
	StorageEndpoint string `yaml:"storageEndpoint"`
}

type localServices struct {
	project        *project.Project
	storage        *SeaweedServer
	membrane       *membrane.Membrane
	status         *LocalServicesStatus
	gateway        *BaseHttpGateway
	storageService *RunStorageService
	dashboard      *dashboard.Dashboard
	isStart        bool
}

func NewLocalServices(project *project.Project, isStart bool, dashboard *dashboard.Dashboard) LocalServices {
	return &localServices{
		project: project,
		isStart: isStart,
		status: &LocalServicesStatus{
			RunDir:          filepath.Join(utils.NitricRunDir(), project.Name),
			MembraneAddress: net.JoinHostPort("localhost", "50051"),
		},
		dashboard: dashboard,
	}
}

func (l *localServices) TriggerAddress() string {
	if l.gateway != nil {
		return l.gateway.GetTriggerAddress()
	}

	return ""
}

func (l *localServices) Refresh() error {
	if l.gateway != nil {
		err := l.gateway.Refresh()
		if err != nil {
			return err
		}
	}

	err := l.dashboard.Refresh(&dashboard.RefreshOptions{
		Pool:            l.GetWorkerPool(),
		TriggerAddress:  l.TriggerAddress(),
		ApiAddresses:    l.Apis(),
		StorageAddress:  l.Status().StorageEndpoint,
		ServiceListener: l.gateway.serviceListener,
	})
	if err != nil {
		return err
	}

	return nil
}

func (l *localServices) Stop() error {
	l.membrane.Stop()
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
	if l.gateway != nil {
		return l.gateway.GetApiAddresses()
	}

	return nil
}

func (l *localServices) HttpWorkers() map[int]string {
	if l.gateway != nil {
		return l.gateway.GetHttpWorkerAddresses()
	}

	return nil
}

func (l *localServices) Status() *LocalServicesStatus {
	return l.status
}

func (l *localServices) GetDashPort() *int {
	if l.gateway != nil {
		return &l.gateway.dashPort
	}

	return nil
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

	l.storageService, err = NewStorage(StorageOptions{
		AccessKey: "dummykey",
		SecretKey: "dummysecret",
		Endpoint:  l.status.StorageEndpoint,
	}, pool)
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

	ev, err := NewEvents(pool, l.project)
	if err != nil {
		return err
	}

	// Start a new gateway plugin
	l.gateway, err = NewGateway()
	if err != nil {
		return err
	}

	// Start local dashboard
	port, err := l.dashboard.Serve(l.storageService)
	if err != nil {
		return err
	}

	l.gateway.dashPort = *port
	l.gateway.project = l.project
	l.gateway.dash = l.dashboard

	// Prepare development membrane to start
	// This will start a single membrane that all
	// running functions will connect to
	l.membrane, err = membrane.New(&membrane.MembraneOptions{
		ServiceAddress:          "0.0.0.0:50051",
		SecretPlugin:            secp,
		QueuePlugin:             qp,
		StoragePlugin:           l.storageService,
		DocumentPlugin:          dp,
		GatewayPlugin:           l.gateway,
		EventsPlugin:            ev,
		ResourcesPlugin:         NewResources(l, l.isStart),
		Pool:                    pool,
		TolerateMissingServices: false,
	})
	if err != nil {
		return err
	}

	return l.membrane.Start()
}

func (l *localServices) GetWorkerPool() pool.WorkerPool {
	return l.gateway.pool
}
