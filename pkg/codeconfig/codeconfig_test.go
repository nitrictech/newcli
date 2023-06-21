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
	"reflect"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/go-cmp/cmp"

	"github.com/nitrictech/cli/pkg/project"
	v1 "github.com/nitrictech/nitric/core/pkg/api/nitric/v1"
	"github.com/nitrictech/nitric/core/pkg/worker"
	"github.com/nitrictech/nitric/core/pkg/worker/adapter"
	"github.com/nitrictech/nitric/core/pkg/worker/pool"
)

func Test_splitPath(t *testing.T) {
	tests := []struct {
		name       string
		workerPath string
		want       string
		want1      openapi3.Parameters
	}{
		{
			name:       "root",
			workerPath: "/",
			want:       "/",
			want1:      openapi3.Parameters{},
		},
		{
			name:       "root with param",
			workerPath: "/:thing",
			want:       "/{thing}",
			want1: openapi3.Parameters{
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						In:       "path",
						Name:     "thing",
						Required: true,
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{
								Type: "string",
							},
						},
					},
				},
			},
		},
		{
			name:       "simple",
			workerPath: "/orders",
			want:       "/orders",
			want1:      openapi3.Parameters{},
		},
		{
			name:       "trailing slash",
			workerPath: "/orders/",
			want:       "/orders",
			want1:      openapi3.Parameters{},
		},
		{
			name:       "with param",
			workerPath: "/orders/:id",
			want:       "/orders/{id}",
			want1: openapi3.Parameters{
				&openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						In:       "path",
						Name:     "id",
						Required: true,
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{
								Type: "string",
							},
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := splitPath(tt.workerPath)
			if got != tt.want {
				t.Errorf("splitPath() got = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(got1, tt.want1) {
				t.Errorf("splitPath() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

func Test_specFromWorkerPool(t *testing.T) {
	tests := []struct {
		name   string
		pool   pool.WorkerPool
		expect *SpecResult
	}{
		{
			name: "Route, Schedule, Subscription and BucketNotification Workers",
			pool: func() pool.WorkerPool {
				workerPool := pool.NewProcessPool(&pool.ProcessPoolOptions{MaxWorkers: 99})

				err := workerPool.AddWorker(worker.NewRouteWorker(&adapter.GrpcAdapter{}, &worker.RouteWorkerOptions{
					Api:     "test-api",
					Path:    "/my-test-path",
					Methods: []string{"PUT", "GET", "POST"},
				}))
				if err != nil {
					t.Fatal(err)
				}

				err = workerPool.AddWorker(worker.NewScheduleWorker(&adapter.GrpcAdapter{}, &worker.ScheduleWorkerOptions{
					Key: "test-schedule",
				}))
				if err != nil {
					t.Fatal(err)
				}

				err = workerPool.AddWorker(worker.NewSubscriptionWorker(&adapter.GrpcAdapter{}, &worker.SubscriptionWorkerOptions{
					Topic: "test-subscription",
				}))
				if err != nil {
					t.Fatal(err)
				}

				err = workerPool.AddWorker(worker.NewBucketNotificationWorker(&adapter.GrpcAdapter{}, &worker.BucketNotificationWorkerOptions{
					Notification: &v1.BucketNotificationWorker{
						Bucket: "test-bucket",
						Config: &v1.BucketNotificationConfig{
							NotificationPrefixFilter: "*",
							NotificationType:         v1.BucketNotificationType_Created,
						},
					},
				}))
				if err != nil {
					t.Fatal(err)
				}

				err = workerPool.AddWorker(worker.NewHttpWorker(&adapter.GrpcAdapter{}, 3000))
				if err != nil {
					t.Fatal(err)
				}

				err = workerPool.AddWorker(worker.NewHttpWorker(&adapter.GrpcAdapter{}, 8080))
				if err != nil {
					t.Fatal(err)
				}

				return workerPool
			}(),
			expect: &SpecResult{
				Apis: []*openapi3.T{
					{
						OpenAPI:    "3.0.1",
						Components: &openapi3.Components{SecuritySchemes: openapi3.SecuritySchemes{}},
						Info:       &openapi3.Info{Title: "test-api", Version: "v1"},
						Paths: openapi3.Paths{
							"/my-test-path": {
								Get: &openapi3.Operation{
									Extensions:  map[string]any{"x-nitric-target": map[string]string{"name": "", "type": "function"}},
									OperationID: "mytestpathget",
									Responses:   openapi3.Responses{"default": {Value: &openapi3.Response{Description: new(string)}}},
								},
								Post: &openapi3.Operation{
									Extensions:  map[string]any{"x-nitric-target": map[string]string{"name": "", "type": "function"}},
									OperationID: "mytestpathpost",
									Responses:   openapi3.Responses{"default": {Value: &openapi3.Response{Description: new(string)}}},
								},
								Put: &openapi3.Operation{
									Extensions:  map[string]any{"x-nitric-target": map[string]string{"name": "", "type": "function"}},
									OperationID: "mytestpathput",
									Responses:   openapi3.Responses{"default": {Value: &openapi3.Response{Description: new(string)}}},
								},
								Parameters: openapi3.Parameters{},
							},
						},
					},
				},
				Schedules: []*TopicResult{{WorkerKey: "test-schedule", TopicKey: "test-schedule"}},
				BucketNotifications: []*BucketNotification{
					{
						Bucket:                   "test-bucket",
						NotificationType:         "Created",
						NotificationPrefixFilter: "*",
					},
				},
				Topics: []*TopicResult{{WorkerKey: "test-subscription", TopicKey: "test-subscription"}},
				HttpWorkers: []*HttpWorker{{Port:3000},{Port:8080}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cc, err := New(&project.Project{}, map[string]string{})
			if err != nil {
				t.Fatal(err)
			}

			got, err := cc.SpecFromWorkerPool(tt.pool)
			if err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(tt.expect, got) {
				t.Error(cmp.Diff(tt.expect, got, cmp.Exporter(func(x reflect.Type) bool {
					// Return true if the type is openapi3.T or has unexported fields
					return x == reflect.TypeOf(openapi3.T{}) || x.NumField() > 0
				})))
			}
		})
	}
}
