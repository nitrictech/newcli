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
	"fmt"
	"strings"

	"github.com/imdario/mergo"
	multierror "github.com/missionMeteora/toolkit/errors"

	"github.com/nitrictech/cli/pkg/cron"
	"github.com/nitrictech/cli/pkg/project"
)

func (c *codeConfig) ToProject() (*project.Project, error) {
	s := project.New(&project.BaseConfig{Name: c.initialProject.Name, Dir: c.initialProject.Dir})

	err := mergo.Merge(s, c.initialProject)
	if err != nil {
		return nil, err
	}

	errs := multierror.ErrorList{}

	for handler, f := range c.functions {
		topicTriggers := make([]string, 0, len(f.subscriptions)+len(f.schedules))

		for k := range f.apis {
			spec, err := c.apiSpec(k)
			if err != nil {
				return nil, fmt.Errorf("could not build spec for api: %s; %w", k, err)
			}

			s.ApiDocs[k] = spec

			secDefs, err := c.securityDefinitions(k)
			if err != nil {
				return nil, fmt.Errorf("error with security definitions for api: %s; %w", k, err)
			}

			s.SecurityDefinitions[k] = secDefs
		}

		for k := range f.buckets {
			s.Buckets[k] = project.Bucket{}
		}

		for k := range f.collections {
			s.Collections[k] = project.Collection{}
		}

		for k := range f.queues {
			s.Queues[k] = project.Queue{}
		}

		for k := range f.secrets {
			s.Secrets[k] = project.Secret{}
		}

		// Add policies
		s.Policies = append(s.Policies, f.policies...)

		for k, v := range f.schedules {
			// Create a new topic target
			// replace spaced with hyphens
			topicName := strings.ToLower(strings.ReplaceAll(k, " ", "-"))
			s.Topics[topicName] = project.Topic{}

			topicTriggers = append(topicTriggers, topicName)

			var exp string
			if v.GetCron() != nil {
				exp = v.GetCron().Cron
			} else if v.GetRate() != nil {
				e, err := cron.RateToCron(v.GetRate().Rate)
				if err != nil {
					errs.Push(fmt.Errorf("schedule expresson %s is invalid; %w", v.GetRate().Rate, err))
					continue
				}

				exp = e
			} else {
				errs.Push(fmt.Errorf("schedule %s is invalid", v.String()))
				continue
			}

			newS := project.Schedule{
				Expression: exp,
				Target: project.ScheduleTarget{
					Type: "topic",
					Name: topicName,
				},
				Event: project.ScheduleEvent{
					PayloadType: "io.nitric.schedule",
					Payload: map[string]interface{}{
						"schedule": k,
					},
				},
			}

			if current, ok := s.Schedules[k]; ok {
				if err := mergo.Merge(&current, &newS); err != nil {
					errs.Push(err)
				} else {
					s.Schedules[k] = current
				}
			} else {
				s.Schedules[k] = newS
			}
		}

		for k := range f.topics {
			s.Topics[k] = project.Topic{}
		}

		for k := range f.subscriptions {
			if _, ok := f.topics[k]; !ok {
				errs.Push(fmt.Errorf("subscription to topic %s defined, but topic does not exist", k))
			} else {
				topicTriggers = append(topicTriggers, k)
			}
		}

		fun, ok := s.Functions[f.name]
		if !ok {
			fun, err = project.FunctionFromHandler(handler)
			if err != nil {
				errs.Push(fmt.Errorf("can not create function from %s %w", handler, err))
				continue
			}
		}

		fun.ComputeUnit.Triggers = project.Triggers{
			Topics: topicTriggers,
		}

		// set the functions worker count
		fun.WorkerCount = f.WorkerCount()
		s.Functions[f.name] = fun
	}

	return s, errs.Err()
}
