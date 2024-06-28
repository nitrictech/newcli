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

package collector

import (
	"context"
	"fmt"
	"sync"

	"github.com/samber/lo"
	"google.golang.org/grpc"

	"github.com/nitrictech/cli/pkg/view/tui/components/view"
	apispb "github.com/nitrictech/nitric/core/pkg/proto/apis/v1"
	httppb "github.com/nitrictech/nitric/core/pkg/proto/http/v1"
	resourcespb "github.com/nitrictech/nitric/core/pkg/proto/resources/v1"
	schedulespb "github.com/nitrictech/nitric/core/pkg/proto/schedules/v1"
	storagepb "github.com/nitrictech/nitric/core/pkg/proto/storage/v1"
	topicspb "github.com/nitrictech/nitric/core/pkg/proto/topics/v1"
	websocketspb "github.com/nitrictech/nitric/core/pkg/proto/websockets/v1"
)

// ServiceRequirements - Cloud resource requirements for a Nitric Application Service
//
// Hosts all Nitric resource servers in a collection-only mode, where services can call into the servers to request resources they require for their operation.
type ServiceRequirements struct {
	serviceName string
	serviceType string
	serviceFile string

	resourceLock sync.Mutex

	routes        map[string][]*apispb.RegistrationRequest
	schedules     map[string]*schedulespb.RegistrationRequest
	subscriptions map[string][]*topicspb.RegistrationRequest
	websockets    map[string][]*websocketspb.RegistrationRequest
	listeners     map[string]*storagepb.RegistrationRequest

	proxy                 *httppb.HttpProxyRequest
	apis                  map[string]*resourcespb.ApiResource
	apiSecurityDefinition map[string]map[string]*resourcespb.ApiSecurityDefinitionResource
	buckets               map[string]*resourcespb.BucketResource
	keyValueStores        map[string]*resourcespb.KeyValueStoreResource
	topics                map[string]*resourcespb.TopicResource
	queues                map[string]*resourcespb.QueueResource
	sqlDatabases          map[string]*resourcespb.SqlDatabaseResource

	policies []*resourcespb.PolicyResource
	secrets  map[string]*resourcespb.SecretResource

	errors []error
	topicspb.UnimplementedTopicsServer
	storagepb.UnimplementedStorageListenerServer
	websocketspb.UnimplementedWebsocketServer

	ApiServer apispb.ApiServer
}

var (
	// _ apispb.ApiServer                    = (*ServiceRequirements)(nil)
	_ schedulespb.SchedulesServer         = (*ServiceRequirements)(nil)
	_ topicspb.SubscriberServer           = (*ServiceRequirements)(nil)
	_ topicspb.TopicsServer               = (*ServiceRequirements)(nil)
	_ storagepb.StorageListenerServer     = (*ServiceRequirements)(nil)
	_ websocketspb.WebsocketHandlerServer = (*ServiceRequirements)(nil)
)

var _ resourcespb.ResourcesServer = (*ServiceRequirements)(nil)

// Error - Returns an error if any requirements have been registered incorrectly, such as duplicates
func (s *ServiceRequirements) Error() error {
	if len(s.errors) > 0 {
		errorView := view.New()
		errorView.Addln("Errors found in service %s", s.serviceFile)

		for _, err := range s.errors {
			errorView.Addln("- %s", err.Error())
		}

		return fmt.Errorf(errorView.Render())
	}

	return nil
}

// TODO: Remove when databases are no longer in preview
func (s *ServiceRequirements) HasDatabases() bool {
	return len(s.sqlDatabases) > 0
}

func (s *ServiceRequirements) WorkerCount() int {
	workerCount := len(lo.Values(s.routes)) +
		len(s.listeners) +
		len(s.schedules) +
		len(lo.Values(s.subscriptions)) +
		len(lo.Values(s.websockets))

	if s.proxy != nil {
		workerCount++
	}

	return workerCount
}

func (s *ServiceRequirements) Declare(ctx context.Context, req *resourcespb.ResourceDeclareRequest) (*resourcespb.ResourceDeclareResponse, error) {
	s.resourceLock.Lock()
	defer s.resourceLock.Unlock()

	switch req.Id.Type {
	case resourcespb.ResourceType_Bucket:
		// Add a bucket
		s.buckets[req.Id.GetName()] = req.GetBucket()
	case resourcespb.ResourceType_KeyValueStore:
		// Add a key/value store
		s.keyValueStores[req.Id.GetName()] = req.GetKeyValueStore()
	case resourcespb.ResourceType_Api:
		// Add an api
		s.apis[req.Id.GetName()] = req.GetApi()
	case resourcespb.ResourceType_ApiSecurityDefinition:
		// Add an api security definition
		if s.apiSecurityDefinition[req.GetApiSecurityDefinition().GetApiName()] == nil {
			s.apiSecurityDefinition[req.GetApiSecurityDefinition().GetApiName()] = make(map[string]*resourcespb.ApiSecurityDefinitionResource)
		}

		s.apiSecurityDefinition[req.GetApiSecurityDefinition().GetApiName()][req.Id.GetName()] = req.GetApiSecurityDefinition()
	case resourcespb.ResourceType_Secret:
		// Add a secret
		s.secrets[req.Id.GetName()] = req.GetSecret()

	case resourcespb.ResourceType_SqlDatabase:
		// Add a sql database
		s.sqlDatabases[req.Id.GetName()] = req.GetSqlDatabase()
	case resourcespb.ResourceType_Policy:
		// Services don't know their own name, so we need to add it here
		if req.GetPolicy().GetPrincipals() == nil {
			req.GetPolicy().Principals = []*resourcespb.ResourceIdentifier{{
				Name: s.serviceName,
				Type: resourcespb.ResourceType_Service,
			}}
		} else {
			for _, principal := range req.GetPolicy().GetPrincipals() {
				if principal.GetName() == "" && principal.GetType() == resourcespb.ResourceType_Service {
					principal.Name = s.serviceName
				}
			}
		}

		// Add a policy
		s.policies = append(s.policies, req.GetPolicy())
	case resourcespb.ResourceType_Topic:
		// add a topic
		s.topics[req.Id.GetName()] = req.GetTopic()
	case resourcespb.ResourceType_Queue:
		// add a queue
		s.queues[req.Id.GetName()] = req.GetQueue()
	}

	return &resourcespb.ResourceDeclareResponse{}, nil
}

func (s *ServiceRequirements) Proxy(stream httppb.Http_ProxyServer) error {
	s.resourceLock.Lock()
	defer s.resourceLock.Unlock()

	// capture a http proxy
	if len(s.routes) > 0 {
		s.errors = append(s.errors, fmt.Errorf("cannot register HTTP proxy, API routes have already been registered"))
	}

	if s.proxy != nil {
		s.errors = append(s.errors, fmt.Errorf("cannot register HTTP proxy, another proxy has already been registered"))
	}

	msg, err := stream.Recv()
	if err != nil {
		return err
	}

	registrationRequest := msg.GetRequest()
	if registrationRequest == nil {
		return fmt.Errorf("first message must be a registration request")
	}

	s.proxy = registrationRequest

	return nil
}

func (s *ServiceRequirements) Serve(stream apispb.Api_ServeServer) error {
	s.resourceLock.Lock()
	defer s.resourceLock.Unlock()

	msg, err := stream.Recv()
	if err != nil {
		return err
	}

	registrationRequest := msg.GetRegistrationRequest()

	if registrationRequest == nil {
		return fmt.Errorf("first message must be a registration request")
	}

	existingRoute, found := lo.Find(s.routes[registrationRequest.Api], func(item *apispb.RegistrationRequest) bool {
		return len(lo.Intersect(item.Methods, registrationRequest.Methods)) > 0 && item.Path == registrationRequest.Path
	})

	if found {
		conflictingMethods := lo.Intersect(existingRoute.Methods, registrationRequest.Methods)
		for _, conflictingMethod := range conflictingMethods {
			s.errors = append(s.errors, fmt.Errorf("%s: %s already registered for API '%s'", conflictingMethod, existingRoute.Path, existingRoute.Api))
		}
	} else {
		s.routes[registrationRequest.Api] = append(s.routes[registrationRequest.Api], registrationRequest)
	}

	return stream.Send(&apispb.ServerMessage{
		Content: &apispb.ServerMessage_RegistrationResponse{
			RegistrationResponse: &apispb.RegistrationResponse{},
		},
	})
}

func (s *ServiceRequirements) Schedule(stream schedulespb.Schedules_ScheduleServer) error {
	s.resourceLock.Lock()
	defer s.resourceLock.Unlock()

	msg, err := stream.Recv()
	if err != nil {
		return err
	}

	registrationRequest := msg.GetRegistrationRequest()

	if registrationRequest == nil {
		return fmt.Errorf("first message must be a registration request")
	}

	_, found := s.schedules[registrationRequest.ScheduleName]
	if found {
		s.errors = append(s.errors, fmt.Errorf("schedule '%s' already registered", registrationRequest.ScheduleName))
	}

	s.schedules[registrationRequest.ScheduleName] = registrationRequest

	return stream.Send(&schedulespb.ServerMessage{
		Content: &schedulespb.ServerMessage_RegistrationResponse{
			RegistrationResponse: &schedulespb.RegistrationResponse{},
		},
	})
}

func (s *ServiceRequirements) Subscribe(stream topicspb.Subscriber_SubscribeServer) error {
	s.resourceLock.Lock()
	defer s.resourceLock.Unlock()

	msg, err := stream.Recv()
	if err != nil {
		return err
	}

	registrationRequest := msg.GetRegistrationRequest()

	if registrationRequest == nil {
		return fmt.Errorf("first message must be a registration request")
	}

	s.subscriptions[registrationRequest.TopicName] = append(s.subscriptions[registrationRequest.TopicName], registrationRequest)

	return stream.Send(&topicspb.ServerMessage{
		Content: &topicspb.ServerMessage_RegistrationResponse{
			RegistrationResponse: &topicspb.RegistrationResponse{},
		},
	})
}

func (s *ServiceRequirements) Listen(stream storagepb.StorageListener_ListenServer) error {
	s.resourceLock.Lock()
	defer s.resourceLock.Unlock()

	msg, err := stream.Recv()
	if err != nil {
		return err
	}

	registrationRequest := msg.GetRegistrationRequest()

	if registrationRequest == nil {
		return fmt.Errorf("first message must be a registration request")
	}

	_, found := s.listeners[registrationRequest.BucketName]

	if found {
		s.errors = append(s.errors, fmt.Errorf("listener for bucket '%s' already registered, only one listener per service is permitted for each bucket", registrationRequest.BucketName))
	} else {
		s.listeners[registrationRequest.BucketName] = registrationRequest
	}

	return stream.Send(&storagepb.ServerMessage{
		Content: &storagepb.ServerMessage_RegistrationResponse{
			RegistrationResponse: &storagepb.RegistrationResponse{},
		},
	})
}

func (s *ServiceRequirements) RegisterServices(grpcServer *grpc.Server) {
	resourcespb.RegisterResourcesServer(grpcServer, s)
	apispb.RegisterApiServer(grpcServer, s.ApiServer)
	schedulespb.RegisterSchedulesServer(grpcServer, s)
	topicspb.RegisterTopicsServer(grpcServer, s)
	topicspb.RegisterSubscriberServer(grpcServer, s)
	websocketspb.RegisterWebsocketHandlerServer(grpcServer, s)
	storagepb.RegisterStorageListenerServer(grpcServer, s)
	httppb.RegisterHttpServer(grpcServer, s)
}

func (s *ServiceRequirements) HandleEvents(stream websocketspb.WebsocketHandler_HandleEventsServer) error {
	s.resourceLock.Lock()
	defer s.resourceLock.Unlock()

	msg, err := stream.Recv()
	if err != nil {
		return err
	}

	registrationRequest := msg.GetRegistrationRequest()

	if registrationRequest == nil {
		return fmt.Errorf("first message must be a registration request")
	}

	existingSocketHandler, found := lo.Find(s.websockets[registrationRequest.SocketName], func(item *websocketspb.RegistrationRequest) bool {
		return item.EventType == registrationRequest.EventType
	})

	if found {
		s.errors = append(s.errors, fmt.Errorf("'%s' handler already registered for websocket '%s'", existingSocketHandler.EventType, existingSocketHandler.SocketName))
	} else {
		s.websockets[registrationRequest.SocketName] = append(s.websockets[registrationRequest.SocketName], registrationRequest)
	}

	return stream.Send(&websocketspb.ServerMessage{
		Content: &websocketspb.ServerMessage_RegistrationResponse{
			RegistrationResponse: &websocketspb.RegistrationResponse{},
		},
	})
}

func NewServiceRequirements(serviceName string, serviceFile string, serviceType string) *ServiceRequirements {
	if serviceType == "" {
		serviceType = "default"
	}

	requirements := &ServiceRequirements{
		serviceName:           serviceName,
		serviceType:           serviceType,
		serviceFile:           serviceFile,
		resourceLock:          sync.Mutex{},
		routes:                make(map[string][]*apispb.RegistrationRequest),
		schedules:             make(map[string]*schedulespb.RegistrationRequest),
		subscriptions:         make(map[string][]*topicspb.RegistrationRequest),
		websockets:            make(map[string][]*websocketspb.RegistrationRequest),
		buckets:               make(map[string]*resourcespb.BucketResource),
		keyValueStores:        make(map[string]*resourcespb.KeyValueStoreResource),
		topics:                make(map[string]*resourcespb.TopicResource),
		policies:              []*resourcespb.PolicyResource{},
		secrets:               make(map[string]*resourcespb.SecretResource),
		listeners:             make(map[string]*storagepb.RegistrationRequest),
		apis:                  make(map[string]*resourcespb.ApiResource),
		sqlDatabases:          make(map[string]*resourcespb.SqlDatabaseResource),
		apiSecurityDefinition: make(map[string]map[string]*resourcespb.ApiSecurityDefinitionResource),
		queues:                make(map[string]*resourcespb.QueueResource),
		errors:                []error{},
	}
	requirements.ApiServer = &ApiCollectorServer{
		requirements: requirements,
	}

	return requirements
}
