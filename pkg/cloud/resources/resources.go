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

package resources

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/asaskevich/EventBus"

	"github.com/nitrictech/cli/pkg/cloud/gateway"
	"github.com/nitrictech/cli/pkg/grpcx"
	"github.com/nitrictech/cli/pkg/validation"
	resourcespb "github.com/nitrictech/nitric/core/pkg/proto/resources/v1"
)

type ResourceName = string

type LocalResourcesState struct {
	Buckets                *ResourceRegistrar[resourcespb.BucketResource]
	KeyValueStores         *ResourceRegistrar[resourcespb.KeyValueStoreResource]
	Policies               *ResourceRegistrar[resourcespb.PolicyResource]
	Secrets                *ResourceRegistrar[resourcespb.SecretResource]
	Topics                 *ResourceRegistrar[resourcespb.TopicResource]
	Queues                 *ResourceRegistrar[resourcespb.QueueResource]
	SqlDatabases           *ResourceRegistrar[resourcespb.SqlDatabaseResource]
	ApiSecurityDefinitions *ResourceRegistrar[resourcespb.ApiSecurityDefinitionResource]
	Errors                 *ResourceRegistrar[error]
}

type LocalResourcesService struct {
	gateway *gateway.LocalGatewayService

	state LocalResourcesState

	bus EventBus.Bus
}

type LocalResourcesOptions struct {
	Gateway *gateway.LocalGatewayService
}

const localResourcesTopic = "local_resources"

func (s *LocalResourcesService) SubscribeToState(fn func(lrs LocalResourcesState)) {
	_ = s.bus.Subscribe(localResourcesTopic, fn)
}

// policyResourceName generates a unique name for a policy resource by hashing the policy document
func policyResourceName(policy *resourcespb.PolicyResource) (string, error) {
	policyDoc, err := json.Marshal(policy)
	if err != nil {
		return "", err
	}

	hasher := md5.New()
	hasher.Write(policyDoc)

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (l *LocalResourcesService) Declare(ctx context.Context, req *resourcespb.ResourceDeclareRequest) (*resourcespb.ResourceDeclareResponse, error) {
	serviceName, err := grpcx.GetServiceNameFromIncomingContext(ctx)
	if err != nil {
		return nil, err
	}

	if !validation.IsValidResourceName(req.Id.Name) {
		err := fmt.Errorf("Invalid resource name: %s", req.Id.Name)
		// register as an error against the service
		l.state.Errors.Register(req.Id.Name, serviceName, &err)
	}

	switch req.Id.Type {
	case resourcespb.ResourceType_Bucket:
		err = l.state.Buckets.Register(req.Id.Name, serviceName, req.GetBucket())
	case resourcespb.ResourceType_KeyValueStore:
		err = l.state.KeyValueStores.Register(req.Id.Name, serviceName, req.GetKeyValueStore())
	case resourcespb.ResourceType_Policy:
		// Services don't know their own name, so forgetful 🙄, that's ok, we'll add it here.
		for _, principal := range req.GetPolicy().Principals {
			if principal.Type == resourcespb.ResourceType_Service {
				principal.Name = serviceName
			}
		}

		policyName, policyErr := policyResourceName(req.GetPolicy())
		if policyErr != nil {
			return nil, policyErr
		}

		err = l.state.Policies.Register(policyName, serviceName, req.GetPolicy())
	case resourcespb.ResourceType_Secret:
		err = l.state.Secrets.Register(req.Id.Name, serviceName, req.GetSecret())
	case resourcespb.ResourceType_Topic:
		err = l.state.Topics.Register(req.Id.Name, serviceName, req.GetTopic())
	case resourcespb.ResourceType_Queue:
		err = l.state.Queues.Register(req.Id.Name, serviceName, req.GetQueue())
	case resourcespb.ResourceType_ApiSecurityDefinition:
		err = l.state.ApiSecurityDefinitions.Register(req.Id.Name, serviceName, req.GetApiSecurityDefinition())
	case resourcespb.ResourceType_SqlDatabase:
		err = l.state.SqlDatabases.Register(req.Id.Name, serviceName, req.GetSqlDatabase())
	}

	if err != nil {
		return nil, err
	}

	l.bus.Publish(localResourcesTopic, l.state)

	return &resourcespb.ResourceDeclareResponse{}, nil
}

// ClearServiceResources - Clear all resources registered by a service, typically done when the service terminates or is restarted
func (l *LocalResourcesService) ClearServiceResources(serviceName string) {
	l.state.Buckets.ClearRequestingService(serviceName)
	l.state.KeyValueStores.ClearRequestingService(serviceName)
	l.state.Policies.ClearRequestingService(serviceName)
	l.state.Secrets.ClearRequestingService(serviceName)
	l.state.Topics.ClearRequestingService(serviceName)
	l.state.Queues.ClearRequestingService(serviceName)
	l.state.ApiSecurityDefinitions.ClearRequestingService(serviceName)
	l.state.Errors.ClearRequestingService(serviceName)
}

func NewLocalResourcesService(opts LocalResourcesOptions) *LocalResourcesService {
	return &LocalResourcesService{
		state: LocalResourcesState{
			Buckets:                NewResourceRegistrar[resourcespb.BucketResource](),
			KeyValueStores:         NewResourceRegistrar[resourcespb.KeyValueStoreResource](),
			Policies:               NewResourceRegistrar[resourcespb.PolicyResource](),
			Secrets:                NewResourceRegistrar[resourcespb.SecretResource](),
			Topics:                 NewResourceRegistrar[resourcespb.TopicResource](),
			Queues:                 NewResourceRegistrar[resourcespb.QueueResource](),
			ApiSecurityDefinitions: NewResourceRegistrar[resourcespb.ApiSecurityDefinitionResource](),
			SqlDatabases:           NewResourceRegistrar[resourcespb.SqlDatabaseResource](),
			Errors:                 []error{},
		},
		gateway: opts.Gateway,
		bus:     EventBus.New(),
	}
}
