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

package codeconfig

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v1 "github.com/nitrictech/nitric/core/pkg/api/nitric/v1"
)

type Server struct {
	name     string
	function *FunctionDependencies
	v1.UnimplementedFaasServiceServer
	v1.UnimplementedResourceServiceServer
}

// TriggerStream - Starts a new FaaS server stream
//
// The deployment server collects information from stream InitRequests, then immediately terminates the stream
// This behavior captures enough information to identify function handlers, without executing the handler code
// during the build process.
func (s *Server) TriggerStream(stream v1.FaasService_TriggerStreamServer) error {
	cm, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.Internal, "error reading message from stream: %v", err)
	}

	ir := cm.GetInitRequest()
	if ir == nil {
		// SHUT IT DOWN!!!!
		// The first message must be an init request from the prospective FaaS worker
		return status.Error(codes.FailedPrecondition, "first message must be InitRequest")
	}

	switch w := ir.Worker.(type) {
	case *v1.InitRequest_Api:
		s.function.AddApiHandler(w.Api)
	case *v1.InitRequest_Schedule:
		s.function.AddScheduleHandler(w.Schedule)
	case *v1.InitRequest_Subscription:
		s.function.AddSubscriptionHandler(w.Subscription)
	case *v1.InitRequest_BucketNotification:
		s.function.AddBucketNotificationHandler(w.BucketNotification)
	case *v1.InitRequest_HttpWorker:
		s.function.AddHttpWorker(w.HttpWorker)
	default:
		s.function.AddError("declared unknown worker type, your CLI version may be out of date with your SDK version")
	}

	return nil
}

// Declare - Accepts resource declarations, adding them as dependencies to the Function
func (s *Server) Declare(ctx context.Context, req *v1.ResourceDeclareRequest) (*v1.ResourceDeclareResponse, error) {
	switch req.Resource.Type {
	case v1.ResourceType_Bucket:
		s.function.AddBucket(req.Resource.Name, req.GetBucket())
	case v1.ResourceType_Collection:
		s.function.AddCollection(req.Resource.Name, req.GetCollection())
	case v1.ResourceType_Queue:
		s.function.AddQueue(req.Resource.Name, req.GetQueue())
	case v1.ResourceType_Topic:
		s.function.AddTopic(req.Resource.Name, req.GetTopic())
	case v1.ResourceType_Policy:
		s.function.AddPolicy(req.GetPolicy())
	case v1.ResourceType_Secret:
		s.function.AddSecret(req.Resource.Name, req.GetSecret())
	case v1.ResourceType_Api:
		s.function.AddApiSecurityDefinitions(req.Resource.Name, req.GetApi().SecurityDefinitions)
		s.function.AddApiSecurity(req.Resource.Name, req.GetApi().Security)
	}

	return &v1.ResourceDeclareResponse{}, nil
}

// NewServer - Creates a new deployment server
func NewServer(name string, function *FunctionDependencies) *Server {
	return &Server{
		name:     name,
		function: function,
	}
}
