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
	"context"
	"fmt"
	"strings"

	v1 "github.com/nitrictech/nitric/core/pkg/api/nitric/v1"
	"github.com/nitrictech/nitric/core/pkg/plugins/resource"
)

type RunResourcesService struct {
	ls      *localServices
	isStart bool
}

var _ resource.ResourceService = &RunResourcesService{}

func (r *RunResourcesService) getApiDetails(name string) (*resource.DetailsResponse[any], error) {
	gatewayUri, ok := r.ls.gateway.GetApiAddresses()[name]
	if !ok {
		return nil, fmt.Errorf("api %s does not exist", name)
	}

	if !r.isStart {
		gatewayUri = strings.Replace(gatewayUri, "localhost", "host.docker.internal", 1)
	}

	return &resource.DetailsResponse[any]{
		Id:       name,
		Provider: "dev",
		Service:  "Api",
		Detail: resource.ApiDetails{
			URL: gatewayUri,
		},
	}, nil
}

func (r *RunResourcesService) Details(ctx context.Context, typ resource.ResourceType, name string) (*resource.DetailsResponse[any], error) {
	switch typ {
	case resource.ResourceType_Api:
		return r.getApiDetails(name)
	default:
		return nil, fmt.Errorf("unsupported resource type %s", typ)
	}
}

func (r *RunResourcesService) Declare(ctx context.Context, req resource.ResourceDeclareRequest) error {
	resource := req.Resource

	switch resource.Type {
	case v1.ResourceType_Bucket:
		r.ls.dashboard.AddBucket(resource.GetName())
	case v1.ResourceType_Queue:
		r.ls.dashboard.AddQueue(resource.GetName())
	case v1.ResourceType_Collection:
		r.ls.dashboard.AddCollection(resource.GetName())
	case v1.ResourceType_Secret:
		r.ls.dashboard.AddSecret(resource.GetName())
	}

	return nil
}

func NewResources(ls *localServices, isStart bool) resource.ResourceService {
	return &RunResourcesService{
		ls:      ls,
		isStart: isStart,
	}
}
