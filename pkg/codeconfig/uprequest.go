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
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/imdario/mergo"
	multierror "github.com/missionMeteora/toolkit/errors"
	"github.com/samber/lo"
	"golang.org/x/exp/slices"

	"github.com/nitrictech/cli/pkg/cron"
	deploy "github.com/nitrictech/nitric/core/pkg/api/nitric/deploy/v1"
	v1 "github.com/nitrictech/nitric/core/pkg/api/nitric/v1"
)

type upRequestBuilder struct {
	projName  string
	resources map[v1.ResourceType]map[string]*deploy.Resource
}

func (b *upRequestBuilder) set(r *deploy.Resource) {
	if _, ok := b.resources[r.Type]; !ok {
		b.resources[r.Type] = map[string]*deploy.Resource{}
	}

	if _, ok := b.resources[r.Type][r.Name]; !ok {
		b.resources[r.Type][r.Name] = r
	} else {
		current := b.resources[r.Type][r.Name]
		if err := mergo.Merge(current, r, mergo.WithAppendSlice); err != nil {
			current = r
		}

		b.resources[r.Type][r.Name] = current
	}
}

func ValidateUpRequest(request *deploy.DeployUpRequest) error {
	errors := []string{}

	websockets := lo.Filter(request.Spec.Resources, func(res *deploy.Resource, idx int) bool {
		return res.Type == v1.ResourceType_Websocket
	})

	for _, ws := range websockets {
		if ws.GetWebsocket().ConnectTarget == nil || ws.GetWebsocket().DisconnectTarget == nil || ws.GetWebsocket().MessageTarget == nil {
			errors = append(errors, fmt.Sprintf("socket: %s, is missing handlers. Sockets must have handlers for connect/disconnect/message events", ws.Name))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("application contains errors:\n %s", strings.Join(errors, "\n"))
	}

	return nil
}

func (b *upRequestBuilder) Output() *deploy.DeployUpRequest {
	res := []*deploy.Resource{}

	for _, resMap := range b.resources {
		for _, r := range resMap {
			res = append(res, r)
		}
	}

	return &deploy.DeployUpRequest{
		Spec: &deploy.Spec{
			Resources: res,
		},
	}
}

func md5Hash(b []byte) string {
	hasher := md5.New()
	hasher.Write(b)

	return hex.EncodeToString(hasher.Sum(nil))
}

func policyResourceName(policy *v1.PolicyResource) (string, error) {
	policyDoc, err := json.Marshal(policy)
	if err != nil {
		return "", err
	}

	return md5Hash(policyDoc), nil
}

func (c *codeConfig) ToUpRequest() (*deploy.DeployUpRequest, error) {
	builder := &upRequestBuilder{
		projName:  c.initialProject.Name,
		resources: map[v1.ResourceType]map[string]*deploy.Resource{},
	}
	errs := multierror.ErrorList{}

	for _, f := range c.functions {
		for bucketName := range f.buckets {
			notifications := []*deploy.BucketNotificationTarget{}

			for _, v := range f.bucketNotifications[bucketName] {
				notifications = append(notifications, &deploy.BucketNotificationTarget{
					Config: v.Config,
					Target: &deploy.BucketNotificationTarget_ExecutionUnit{
						ExecutionUnit: f.name,
					},
				})
			}

			res := &deploy.Resource{
				Name: bucketName,
				Type: v1.ResourceType_Bucket,
				Config: &deploy.Resource_Bucket{
					Bucket: &deploy.Bucket{
						Notifications: notifications,
					},
				},
			}

			builder.set(res)
		}

		for k := range f.collections {
			builder.set(&deploy.Resource{
				Name: k,
				Type: v1.ResourceType_Collection,
				Config: &deploy.Resource_Collection{
					Collection: &deploy.Collection{},
				},
			})
		}

		for k := range f.queues {
			builder.set(&deploy.Resource{
				Name: k,
				Type: v1.ResourceType_Queue,
				Config: &deploy.Resource_Queue{
					Queue: &deploy.Queue{},
				},
			})
		}

		for topicName := range f.topics {
			subs := []*deploy.SubscriptionTarget{}

			for _, v := range f.subscriptions {
				if v.Topic == topicName {
					subs = append(subs, &deploy.SubscriptionTarget{
						Target: &deploy.SubscriptionTarget_ExecutionUnit{
							ExecutionUnit: f.name,
						},
					})
				}
			}

			res := &deploy.Resource{
				Name: topicName,
				Type: v1.ResourceType_Topic,
				Config: &deploy.Resource_Topic{
					Topic: &deploy.Topic{
						// TODO: Determine if this will successfully merge between multiple functions
						Subscriptions: subs,
					},
				},
			}
			builder.set(res)
		}

		for k := range f.secrets {
			res := &deploy.Resource{
				Name:   k,
				Type:   v1.ResourceType_Secret,
				Config: &deploy.Resource_Secret{},
			}
			builder.set(res)
		}

		for k := range f.apis {
			spec, err := c.apiSpec(k, nil)
			if err != nil {
				errs.Push(fmt.Errorf("could not build spec for api: %s; %w", k, err))
				continue
			}

			apiBody, err := json.Marshal(spec)
			if err != nil {
				errs.Push(err)
				continue
			}

			builder.set(&deploy.Resource{
				Name: k,
				Type: v1.ResourceType_Api,
				Config: &deploy.Resource_Api{
					Api: &deploy.Api{
						Document: &deploy.Api_Openapi{
							Openapi: string(apiBody),
						},
					},
				},
			})
		}

		// Create websockets and attach relevant workers for this function
		for k, ws := range f.websockets {
			// Collect all sockets organised by name and even type
			deployWebsocket := &deploy.Websocket{}

			if ws.connectWorker != nil {
				deployWebsocket.ConnectTarget = &deploy.WebsocketTarget{
					Target: &deploy.WebsocketTarget_ExecutionUnit{
						ExecutionUnit: f.name,
					},
				}
			}

			if ws.disconnectWorker != nil {
				deployWebsocket.DisconnectTarget = &deploy.WebsocketTarget{
					Target: &deploy.WebsocketTarget_ExecutionUnit{
						ExecutionUnit: f.name,
					},
				}
			}

			if ws.messageWorker != nil {
				deployWebsocket.MessageTarget = &deploy.WebsocketTarget{
					Target: &deploy.WebsocketTarget_ExecutionUnit{
						ExecutionUnit: f.name,
					},
				}
			}

			builder.set(&deploy.Resource{
				Name: k,
				Type: v1.ResourceType_Websocket,
				Config: &deploy.Resource_Websocket{
					Websocket: deployWebsocket,
				},
			})
		}

		// This will produce a compacted map of policy resources with colliding principals and actions
		// we'll compact all these resources into a single policy object
		compactedPoliciesByKey := lo.GroupBy(f.policies, func(item *v1.PolicyResource) string {
			// get the princpals and actions as a unique key (make sure they're sorted for consistency)
			principalNames := lo.Reduce(item.Principals, func(agg []string, principal *v1.Resource, idx int) []string {
				return append(agg, principal.Name)
			}, []string{})
			slices.Sort(principalNames)

			principals := strings.Join(principalNames, ":")

			slices.Sort(item.Actions)
			actions := lo.Reduce(item.Actions, func(agg string, action v1.Action, idx int) string {
				return agg + action.String()
			}, "")

			return principals + "-" + actions
		})

		compactedPolicies := map[string]*v1.PolicyResource{}
		// for each key of the compacted policies we want to make a single policy object that appends all of the policies resources together
		for k, pols := range compactedPoliciesByKey {
			newPol := pols[0]

			for _, pol := range pols[1:] {
				newPol.Resources = append(newPol.Resources, pol.Resources...)
			}

			policyName := md5Hash([]byte(k))

			compactedPolicies[policyName] = newPol
		}

		for policyName, v := range compactedPolicies {
			principals := []*deploy.Resource{}
			resources := []*deploy.Resource{}

			for _, r := range v.Resources {
				resources = append(resources, &deploy.Resource{
					Name: r.Name,
					Type: r.Type,
				})
			}

			for _, p := range v.Principals {
				principals = append(principals, &deploy.Resource{
					Name: p.Name,
					Type: p.Type,
				})
			}

			builder.set(&deploy.Resource{
				Name: policyName,
				Type: v1.ResourceType_Policy,
				Config: &deploy.Resource_Policy{
					Policy: &deploy.Policy{
						Principals: principals,
						Actions:    v.Actions,
						Resources:  resources,
					},
				},
			})
		}

		for k, v := range f.schedules {
			var exp string
			switch v.Cadence.(type) {
			case *v1.ScheduleWorker_Cron:
				exp = v.GetCron().Cron
			default:
				e, err := cron.RateToCron(v.GetRate().Rate)
				if err != nil {
					errs.Push(fmt.Errorf("schedule expresson %s is invalid; %w", v.GetRate().Rate, err))
					continue
				}

				exp = e
			}

			builder.set(&deploy.Resource{
				Name: k,
				Type: v1.ResourceType_Schedule,
				Config: &deploy.Resource_Schedule{
					Schedule: &deploy.Schedule{
						Cron: exp,
						Target: &deploy.ScheduleTarget{
							Target: &deploy.ScheduleTarget_ExecutionUnit{
								ExecutionUnit: f.name,
							},
						},
					},
				},
			})
		}

		for range f.httpWorkers {
			builder.set(&deploy.Resource{
				Name: f.name,
				Type: v1.ResourceType_Http,
				Config: &deploy.Resource_Http{
					Http: &deploy.Http{
						Target: &deploy.HttpTarget{
							Target: &deploy.HttpTarget_ExecutionUnit{
								ExecutionUnit: f.name,
							},
						},
					},
				},
			})
		}

		// Get the original function config
		fun := c.initialProject.Functions[f.name]

		builder.set(&deploy.Resource{
			Name: f.name,
			Type: v1.ResourceType_Function,
			Config: &deploy.Resource_ExecutionUnit{
				ExecutionUnit: &deploy.ExecutionUnit{
					Source: &deploy.ExecutionUnit_Image{
						Image: &deploy.ImageSource{
							Uri: fmt.Sprintf("%s-%s", c.initialProject.Name, f.name),
						},
					},
					Workers: int32(f.WorkerCount()),
					Type:    fun.Config.Type,
					Env:     c.envMap,
				},
			},
		})
	}

	if errs.Err() != nil {
		return nil, errs.Err()
	}

	out := builder.Output()

	err := ValidateUpRequest(out)
	if err != nil {
		return nil, err
	}

	return out, nil
}
