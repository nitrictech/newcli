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
	"sync"

	"github.com/nitrictech/cli/pkg/utils"

	pb "github.com/nitrictech/nitric/pkg/api/nitric/v1"
)

type Api struct {
	workers []*pb.ApiWorker
	lock    sync.RWMutex
}

func (a *Api) String() string {
	return fmt.Sprintf("workers: %+v", a.workers)
}

func newApi() *Api {
	return &Api{
		workers: make([]*pb.ApiWorker, 0),
	}
}

func normalizePath(path string) string {
	parts := utils.SplitPath(path)
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = ":param"
			continue
		}
		parts[i] = strings.ToLower(part)
	}
	return strings.Join(parts, "/")
}

func matchingWorkers(a *pb.ApiWorker, b *pb.ApiWorker) bool {
	if normalizePath(a.GetPath()) == normalizePath(b.GetPath()) {
		for _, aMethod := range a.GetMethods() {
			for _, bMethod := range b.GetMethods() {
				if aMethod == bMethod {
					return true
				}
			}
		}
	}
	return false
}

func (a *Api) AddWorker(worker *pb.ApiWorker) error {
	a.lock.Lock()
	defer a.lock.Unlock()
	// Ensure the worker is unique
	for _, existing := range a.workers {
		if matchingWorkers(existing, worker) {
			return fmt.Errorf("overlapping worker %v already registered, can't add new worker %v", existing, worker)
		}
	}
	a.workers = append(a.workers, worker)
	return nil
}

// FunctionDependencies - Stores information about a Nitric Function, and it's dependencies
type FunctionDependencies struct {
	name          string
	apis          map[string]*Api
	subscriptions map[string]*pb.SubscriptionWorker
	schedules     map[string]*pb.ScheduleWorker
	buckets       map[string]*pb.BucketResource
	topics        map[string]*pb.TopicResource
	collections   map[string]*pb.CollectionResource
	queues        map[string]*pb.QueueResource
	policies      []*pb.PolicyResource
	secrets       map[string]*pb.SecretResource
	lock          sync.RWMutex
}

// AddPolicy - Adds an access policy dependency to the function
func (a *FunctionDependencies) AddPolicy(p *pb.PolicyResource) {
	a.lock.Lock()
	defer a.lock.Unlock()
	for _, p := range p.Principals {
		// If provided a blank function principal assume its for this function
		if p.Type == pb.ResourceType_Function && p.Name == "" {
			p.Name = a.name
		}
	}

	a.policies = append(a.policies, p)
}

func (a *FunctionDependencies) AddApiHandler(aw *pb.ApiWorker) error {
	a.lock.Lock()
	defer a.lock.Unlock()
	if a.apis[aw.Api] == nil {
		a.apis[aw.Api] = newApi()
	}

	return a.apis[aw.Api].AddWorker(aw)
}

// AddSubscriptionHandler - registers a handler in the function that subscribes to a topic of events
func (a *FunctionDependencies) AddSubscriptionHandler(sw *pb.SubscriptionWorker) error {
	a.lock.Lock()
	defer a.lock.Unlock()
	// TODO: Determine if this subscription handler has a write policy to the same topic
	if a.subscriptions[sw.Topic] != nil {
		// return a new error
		return fmt.Errorf("subscription already declared for topic %s, only one subscription per topic is allowed per application", sw.Topic)
	}

	// This maps to a trigger worker for this application
	a.subscriptions[sw.Topic] = sw

	return nil
}

// AddScheduleHandler - registers a handler in the function that runs on a schedule
func (a *FunctionDependencies) AddScheduleHandler(sw *pb.ScheduleWorker) error {
	a.lock.Lock()
	defer a.lock.Unlock()
	if a.schedules[sw.Key] != nil {
		return fmt.Errorf("schedule %s already exists", sw.Key)
	}

	a.schedules[sw.GetKey()] = sw

	return nil
}

// AddBucket - adds a storage bucket dependency to the function
func (a *FunctionDependencies) AddBucket(name string, b *pb.BucketResource) {
	a.lock.Lock()
	defer a.lock.Unlock()
	a.buckets[name] = b
}

// AddTopic - adds a pub/sub topic dependency to the function
func (a *FunctionDependencies) AddTopic(name string, t *pb.TopicResource) {
	a.lock.Lock()
	defer a.lock.Unlock()
	a.topics[name] = t
}

// AddCollection - adds a document database collection dependency to the function
func (a *FunctionDependencies) AddCollection(name string, c *pb.CollectionResource) {
	a.lock.Lock()
	defer a.lock.Unlock()
	a.collections[name] = c
}

// AddQueue - adds a queue dependency to the function
func (a *FunctionDependencies) AddQueue(name string, q *pb.QueueResource) {
	a.lock.Lock()
	defer a.lock.Unlock()
	a.queues[name] = q
}

func (a *FunctionDependencies) AddSecret(name string, s *pb.SecretResource) {
	a.lock.Lock()
	defer a.lock.Unlock()
	a.secrets[name] = s
}

// NewFunction - creates a new Nitric Function, ready to register handlers and dependencies.
func NewFunction(name string) *FunctionDependencies {
	return &FunctionDependencies{
		name:          name,
		apis:          make(map[string]*Api),
		subscriptions: make(map[string]*pb.SubscriptionWorker),
		schedules:     make(map[string]*pb.ScheduleWorker),
		buckets:       make(map[string]*pb.BucketResource),
		topics:        make(map[string]*pb.TopicResource),
		collections:   make(map[string]*pb.CollectionResource),
		queues:        make(map[string]*pb.QueueResource),
		secrets:       make(map[string]*pb.SecretResource),
		policies:      make([]*pb.PolicyResource, 0),
	}
}
