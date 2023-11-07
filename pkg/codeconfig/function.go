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
	"sync"

	"github.com/nitrictech/cli/pkg/preview"
	"github.com/nitrictech/cli/pkg/project"
	v1 "github.com/nitrictech/nitric/core/pkg/api/nitric/v1"
)

// FunctionDependencies - Stores information about a Nitric Function, and it's dependencies
type FunctionDependencies struct {
	name                string
	functionConfig      project.Function
	apis                map[string]*Api
	websockets          map[string]*Websocket
	subscriptions       map[string]*v1.SubscriptionWorker
	schedules           map[string]*v1.ScheduleWorker
	httpWorkers         map[int]*v1.HttpWorker
	buckets             map[string]*v1.BucketResource
	topics              map[string]*v1.TopicResource
	collections         map[string]*v1.CollectionResource
	queues              map[string]*v1.QueueResource
	policies            []*v1.PolicyResource
	secrets             map[string]*v1.SecretResource
	bucketNotifications map[string][]*v1.BucketNotificationWorker
	errors              []string
	lock                sync.RWMutex
}

func (a *FunctionDependencies) AddError(err string) {
	a.errors = append(a.errors, fmt.Sprintf("function %s: %s", a.functionConfig.Handler, err))
}

// AddPolicy - Adds an access policy dependency to the function
func (a *FunctionDependencies) AddPolicy(p *v1.PolicyResource) {
	a.lock.Lock()
	defer a.lock.Unlock()

	for _, p := range p.Principals {
		// If provided a blank function principal assume its for this function
		if p.Type == v1.ResourceType_Function && p.Name == "" {
			p.Name = a.name
		}
	}

	a.policies = append(a.policies, p)
}

func (a *FunctionDependencies) AddApiSecurityDefinitions(name string, sds map[string]*v1.ApiSecurityDefinition) {
	a.lock.Lock()
	defer a.lock.Unlock()

	if a.apis[name] == nil {
		a.apis[name] = newApi(a)
	}

	for n, sd := range sds {
		a.apis[name].AddSecurityDefinition(n, sd)
	}
}

func (a *FunctionDependencies) AddApiSecurity(name string, security map[string]*v1.ApiScopes, cors *v1.ApiCorsDefinition) {
	a.lock.Lock()
	defer a.lock.Unlock()

	if a.apis[name] == nil {
		a.apis[name] = newApi(a)
	}

	a.apis[name].AddCors(cors)

	for n, scopes := range security {
		a.apis[name].AddSecurity(n, scopes.Scopes)
	}
}

func (a *FunctionDependencies) AddApiHandler(aw *v1.ApiWorker) {
	a.lock.Lock()
	defer a.lock.Unlock()

	if len(a.httpWorkers) > 0 {
		a.AddError("APIs cannot be defined in functions that already contain HTTP proxies")
		return
	}

	if a.apis[aw.Api] == nil {
		a.apis[aw.Api] = newApi(a)
	}

	a.apis[aw.Api].AddWorker(aw)
}

func (a *FunctionDependencies) AddWebsocketHandler(ws *v1.WebsocketWorker) {
	a.lock.Lock()
	defer a.lock.Unlock()

	if !a.functionConfig.Project.IsPreviewFeatureEnabled(preview.Feature_Websockets) {
		a.AddError(`websockets are currently in preview if you would like to enable them add
preview-features:
  - websockets
to your nitric.yaml file.
		`)

		return
	}

	if a.websockets[ws.Socket] == nil {
		a.websockets[ws.Socket] = newWebsocket(ws.Socket, a)
	}

	a.websockets[ws.Socket].AddWorker(ws)
}

// AddSubscriptionHandler - registers a handler in the function that subscribes to a topic of events
func (a *FunctionDependencies) AddSubscriptionHandler(sw *v1.SubscriptionWorker) {
	a.lock.Lock()
	defer a.lock.Unlock()

	// TODO: Determine if this subscription handler has a write policy to the same topic
	if a.subscriptions[sw.Topic] != nil {
		// return a new error
		a.AddError(fmt.Sprintf("declared multiple subscriptions for topic %s, only one subscription per topic is allowed per function", sw.Topic))
		return
	}

	a.subscriptions[sw.Topic] = sw
}

func (a *FunctionDependencies) WorkerCount() int {
	workerCount := 0

	for _, v := range a.websockets {
		workerCount = workerCount + v.WorkerCount()
	}

	for _, v := range a.apis {
		workerCount = workerCount + len(v.workers)
	}

	workerCount = workerCount + len(a.subscriptions) + len(a.schedules) + len(a.bucketNotifications) + len(a.httpWorkers)

	return workerCount
}

// AddScheduleHandler - registers a handler in the function that runs on a schedule
func (a *FunctionDependencies) AddScheduleHandler(sw *v1.ScheduleWorker) {
	a.lock.Lock()
	defer a.lock.Unlock()

	if a.schedules[sw.Key] != nil {
		a.AddError(fmt.Sprintf("declared schedule %s multiple times", sw.Key))
		return
	}

	a.schedules[sw.GetKey()] = sw
}

// AddHttpWorker - registers a handler in the function that listens on a port
func (a *FunctionDependencies) AddHttpWorker(hw *v1.HttpWorker) {
	a.lock.Lock()
	defer a.lock.Unlock()

	if !a.functionConfig.Project.IsPreviewFeatureEnabled(preview.Feature_Http) {
		a.AddError(`HTTP Proxies are currently in preview if you would like to enable them add
preview-features:
  - http
to your nitric.yaml file.
		`)

		return
	}

	if len(a.httpWorkers) > 0 {
		a.AddError("declared multiple http proxies, only one http proxy is allowed per function")
	}

	if len(a.apis) > 0 {
		a.AddError("declared a HTTP Proxy, but already declares an API. Function can only handle one")
		return
	}

	a.httpWorkers[int(hw.GetPort())] = hw
}

// AddBucketNotificationHandler - registers a handler in the function that is triggered by bucket events
func (a *FunctionDependencies) AddBucketNotificationHandler(nw *v1.BucketNotificationWorker) {
	a.lock.Lock()
	defer a.lock.Unlock()

	a.bucketNotifications[nw.GetBucket()] = append(a.bucketNotifications[nw.GetBucket()], nw)
}

// AddBucket - adds a storage bucket dependency to the function
func (a *FunctionDependencies) AddBucket(name string, b *v1.BucketResource) {
	a.lock.Lock()
	defer a.lock.Unlock()

	a.buckets[name] = b
}

// AddTopic - adds a pub/sub topic dependency to the function
func (a *FunctionDependencies) AddTopic(name string, t *v1.TopicResource) {
	a.lock.Lock()
	defer a.lock.Unlock()

	a.topics[name] = t
}

// AddCollection - adds a document database collection dependency to the function
func (a *FunctionDependencies) AddCollection(name string, c *v1.CollectionResource) {
	a.lock.Lock()
	defer a.lock.Unlock()

	a.collections[name] = c
}

// AddQueue - adds a queue dependency to the function
func (a *FunctionDependencies) AddQueue(name string, q *v1.QueueResource) {
	a.lock.Lock()
	defer a.lock.Unlock()

	a.queues[name] = q
}

func (a *FunctionDependencies) AddSecret(name string, s *v1.SecretResource) {
	a.lock.Lock()
	defer a.lock.Unlock()

	a.secrets[name] = s
}

// NewFunction - creates a new Nitric Function, ready to register handlers and dependencies.
func NewFunction(name string, projectFunction project.Function) *FunctionDependencies {
	return &FunctionDependencies{
		name:                name,
		functionConfig:      projectFunction,
		apis:                make(map[string]*Api),
		websockets:          make(map[string]*Websocket),
		subscriptions:       make(map[string]*v1.SubscriptionWorker),
		httpWorkers:         make(map[int]*v1.HttpWorker),
		schedules:           make(map[string]*v1.ScheduleWorker),
		buckets:             make(map[string]*v1.BucketResource),
		topics:              make(map[string]*v1.TopicResource),
		collections:         make(map[string]*v1.CollectionResource),
		queues:              make(map[string]*v1.QueueResource),
		secrets:             make(map[string]*v1.SecretResource),
		bucketNotifications: make(map[string][]*v1.BucketNotificationWorker),
		policies:            make([]*v1.PolicyResource, 0),
		errors:              []string{},
	}
}
