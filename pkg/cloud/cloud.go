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

package cloud

import (
	"fmt"
	"io"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/nitrictech/cli/pkg/cloud/apis"
	"github.com/nitrictech/cli/pkg/cloud/gateway"
	"github.com/nitrictech/cli/pkg/cloud/http"
	"github.com/nitrictech/cli/pkg/cloud/keyvalue"
	"github.com/nitrictech/cli/pkg/cloud/queues"
	"github.com/nitrictech/cli/pkg/cloud/resources"
	"github.com/nitrictech/cli/pkg/cloud/schedules"
	"github.com/nitrictech/cli/pkg/cloud/secrets"
	"github.com/nitrictech/cli/pkg/cloud/sql"
	"github.com/nitrictech/cli/pkg/cloud/storage"
	"github.com/nitrictech/cli/pkg/cloud/topics"
	"github.com/nitrictech/cli/pkg/cloud/websockets"
	"github.com/nitrictech/cli/pkg/grpcx"
	"github.com/nitrictech/cli/pkg/netx"
	"github.com/nitrictech/cli/pkg/project/localconfig"
	"github.com/nitrictech/nitric/core/pkg/logger"
	"github.com/nitrictech/nitric/core/pkg/server"
)

type Subscribable[T any, A any] interface {
	SubscribeToState(fn func(T))
	SubscribeToAction(fn func(A)) // used to subscribe to api calls, ws messages, topic deliveries etc
}

type ServiceName = string

type LocalCloud struct {
	serverLock sync.Mutex
	servers    map[ServiceName]*server.NitricServer

	Apis       *apis.LocalApiGatewayService
	KeyValue   *keyvalue.BoltDocService
	Gateway    *gateway.LocalGatewayService
	Http       *http.LocalHttpProxy
	Resources  *resources.LocalResourcesService
	Schedules  *schedules.LocalSchedulesService
	Secrets    *secrets.DevSecretService
	Storage    *storage.LocalStorageService
	Topics     *topics.LocalTopicsAndSubscribersService
	Websockets *websockets.LocalWebsocketService
	Queues     *queues.LocalQueuesService
	Databases  *sql.LocalSqlServer

	// Store all the plugins locally
}

// StartLocalNitric - starts the Nitric Server, including plugins and their local dependencies (e.g. local versions of cloud services)
func (lc *LocalCloud) Stop() {
	for _, m := range lc.servers {
		m.Stop()
	}

	err := lc.Gateway.Stop()
	if err != nil {
		logger.Errorf("Error stopping gateway: %s", err.Error())
	}

	err = lc.Databases.Stop()
	if err != nil {
		logger.Errorf("Error stopping databases: %s", err.Error())
	}
}

func (lc *LocalCloud) AddService(serviceName string) (int, error) {
	lc.serverLock.Lock()
	defer lc.serverLock.Unlock()

	if _, ok := lc.servers[serviceName]; ok {
		return 0, fmt.Errorf("service %s already started", serviceName)
	}

	// get an available port
	ports, err := netx.TakePort(1)
	if err != nil {
		return 0, err
	}

	nitricRuntimeServer, _ := server.New(
		server.WithResourcesPlugin(lc.Resources),
		server.WithApiPlugin(lc.Apis),
		server.WithHttpPlugin(lc.Http),
		server.WithSchedulesPlugin(lc.Schedules),
		server.WithTopicsListenerPlugin(lc.Topics),
		server.WithTopicsPlugin(lc.Topics),
		server.WithStorageListenerPlugin(lc.Storage),
		server.WithWebsocketListenerPlugin(lc.Websockets),
		server.WithSqlPlugin(lc.Databases),
		server.WithServiceAddress(fmt.Sprintf("0.0.0.0:%d", ports[0])),
		server.WithSecretManagerPlugin(lc.Secrets),
		server.WithStoragePlugin(lc.Storage),
		server.WithKeyValuePlugin(lc.KeyValue),
		server.WithGatewayPlugin(lc.Gateway),
		server.WithWebsocketPlugin(lc.Websockets),
		server.WithQueuesPlugin(lc.Queues),
		server.WithMinWorkers(0),
		server.WithChildCommand([]string{}))

	// Create a watcher that clears old resources when the service is restarted
	_, err = resources.NewServiceResourceRefresher(serviceName, resources.NewServiceResourceRefresherArgs{
		Resources:  lc.Resources,
		Apis:       lc.Apis,
		Schedules:  lc.Schedules,
		Http:       lc.Http,
		Listeners:  lc.Storage,
		Websockets: lc.Websockets,
		Topics:     lc.Topics,
		Storage:    lc.Storage,
	})
	if err != nil {
		return 0, err
	}

	go func() {
		interceptor, streamInterceptor := grpcx.CreateServiceNameInterceptor(serviceName)

		srv := grpc.NewServer(
			grpc.UnaryInterceptor(interceptor),
			grpc.StreamInterceptor(streamInterceptor),
		)

		// Enable reflection on the gRPC server for local testing
		reflection.Register(srv)

		err := nitricRuntimeServer.Start(server.WithGrpcServer(srv))
		if err != nil {
			logger.Errorf("Error starting nitric server: %s", err.Error())
		}
	}()

	lc.servers[serviceName] = nitricRuntimeServer

	return ports[0], nil
}

type LocalCloudOptions struct {
	TLSCredentials  *gateway.TLSCredentials
	LogWriter       io.Writer
	LocalConfig     localconfig.LocalConfiguration
	MigrationRunner sql.MigrationRunner
}

func New(projectName string, opts LocalCloudOptions) (*LocalCloud, error) {
	localGateway, err := gateway.NewGateway(gateway.NewGatewayOpts{
		TLSCredentials: opts.TLSCredentials,
		LogWriter:      opts.LogWriter,
		LocalConfig:    opts.LocalConfig,
	})
	if err != nil {
		return nil, err
	}

	localResources := resources.NewLocalResourcesService(resources.LocalResourcesOptions{
		Gateway: localGateway,
	})

	localTopics, err := topics.NewLocalTopicsService()
	if err != nil {
		return nil, err
	}

	localWebsockets, err := websockets.NewLocalWebsocketService()
	if err != nil {
		return nil, err
	}

	localStorage, err := storage.NewLocalStorageService(storage.StorageOptions{
		AccessKey: "dummykey",
		SecretKey: "dummysecret",
	})
	if err != nil {
		return nil, err
	}

	localApis := apis.NewLocalApiGatewayService()
	localSchedules := schedules.NewLocalSchedulesService(localResources.LogServiceError)
	localHttpProxy := http.NewLocalHttpProxyService()

	localSecrets, err := secrets.NewSecretService()
	if err != nil {
		return nil, err
	}

	if opts.LogWriter == nil {
		opts.LogWriter = io.Discard
	}

	keyvalueService, err := keyvalue.NewBoltService()
	if err != nil {
		return nil, err
	}

	localQueueService, err := queues.NewLocalQueuesService()
	if err != nil {
		return nil, err
	}

	localDatabaseService, err := sql.NewLocalSqlServer(projectName, localResources, opts.MigrationRunner)
	if err != nil {
		return nil, err
	}

	return &LocalCloud{
		servers:    make(map[string]*server.NitricServer),
		Apis:       localApis,
		Http:       localHttpProxy,
		Resources:  localResources,
		Schedules:  localSchedules,
		Storage:    localStorage,
		Topics:     localTopics,
		Websockets: localWebsockets,
		Gateway:    localGateway,
		Secrets:    localSecrets,
		KeyValue:   keyvalueService,
		Queues:     localQueueService,
		Databases:  localDatabaseService,
	}, nil
}
