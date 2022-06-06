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

package project

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path"
	"path/filepath"

	"github.com/getkin/kin-openapi/openapi3"
	"gopkg.in/yaml.v2"

	v1 "github.com/nitrictech/nitric/pkg/api/nitric/v1"
)

type Triggers struct {
	Topics []string `yaml:"topics,omitempty"`
}

type ComputeUnit struct {
	Name string `yaml:"-"`

	// Triggers used to invoke this compute unit, e.g. Topic Subscriptions
	Triggers Triggers `yaml:"triggers,omitempty"`

	// The memory of the compute instance in MB
	Memory int `yaml:"memory,omitempty"`

	// The minimum number of instances to keep alive
	MinScale int `yaml:"minScale,omitempty"`

	// The maximum number of instances to scale to
	MaxScale int `yaml:"maxScale,omitempty"`
}

type Function struct {
	// The location of the function handler
	Handler string `yaml:"handler"`

	ComputeUnit `yaml:",inline"`

	// The number of workers this function contains
	WorkerCount int
}

type Container struct {
	Dockerfile string   `yaml:"dockerfile"`
	Args       []string `yaml:"args,omitempty"`

	ComputeUnit `yaml:",inline"`
}

type Compute interface {
	ImageTagName(s *Project, provider string) string
	Unit() *ComputeUnit
	Workers() int
}

// A subset of a NitricEvent
// excluding it's requestId
// This will be generated based on the scedule
// type ScheduleEvent struct {
// 	PayloadType string                 `yaml:"payloadType"`
// 	Payload     map[string]interface{} `yaml:"payload,omitempty"`
// }

type ScheduleTarget struct {
	Type string `yaml:"type"` // "topic" | "function"
	Name string `yaml:"name"`
}

type Schedule struct {
	Expression string `yaml:"expression"`

	// The Topic to be targeted for schedule
	Target ScheduleTarget `yaml:"target"`
	// Event  ScheduleEvent  `yaml:"event"`
}

type Collection struct{}

type Bucket struct{}

type Topic struct{}

type Queue struct{}

type Secret struct{}

type Project struct {
	Dir                 string                                          `yaml:"-"`
	Name                string                                          `yaml:"name"`
	Functions           map[string]Function                             `yaml:"functions,omitempty"`
	Collections         map[string]Collection                           `yaml:"collections,omitempty"`
	Containers          map[string]Container                            `yaml:"containers,omitempty"`
	Buckets             map[string]Bucket                               `yaml:"buckets,omitempty"`
	Topics              map[string]Topic                                `yaml:"topics,omitempty"`
	Queues              map[string]Queue                                `yaml:"queues,omitempty"`
	Schedules           map[string]Schedule                             `yaml:"schedules,omitempty"`
	ApiDocs             map[string]*openapi3.T                          `yaml:"-"`
	SecurityDefinitions map[string]map[string]*v1.ApiSecurityDefinition `yaml:"-"`
	Apis                map[string]string                               `yaml:"apis,omitempty"`
	// TODO: Not currently supported by nitric.yaml configuration (but is technically definable using the proto model)
	// We may want to decouple the definition from contracts at a later stage
	// but re-using the contract here provides us a serializable entity with no
	// repetition/redefinition
	// NOTE: if we want to use the proto definition here we would need support for yaml parsing to use customisable tags
	Policies []*v1.PolicyResource `yaml:"-"`
	Secrets  map[string]Secret    `yaml:"secrets,omitempty"`
}

func New(config *Config) *Project {
	return &Project{
		Name:                config.Name,
		Dir:                 config.Dir,
		Containers:          map[string]Container{},
		Collections:         map[string]Collection{},
		Functions:           map[string]Function{},
		Buckets:             map[string]Bucket{},
		Topics:              map[string]Topic{},
		Queues:              map[string]Queue{},
		Schedules:           map[string]Schedule{},
		Apis:                map[string]string{},
		ApiDocs:             map[string]*openapi3.T{},
		SecurityDefinitions: make(map[string]map[string]*v1.ApiSecurityDefinition),
		Policies:            make([]*v1.PolicyResource, 0),
		Secrets:             map[string]Secret{},
	}
}

func (s *Project) Computes() []Compute {
	computes := []Compute{}
	for _, c := range s.Functions {
		copy := c
		computes = append(computes, &copy)
	}
	for _, c := range s.Containers {
		copy := c
		computes = append(computes, &copy)
	}
	return computes
}

// Compute default policies for a stack
func calculateDefaultPolicies(s *Project) []*v1.PolicyResource {
	policies := make([]*v1.PolicyResource, 0)

	principals := make([]*v1.Resource, 0)

	for name := range s.Functions {
		principals = append(principals, &v1.Resource{
			Name: name,
			Type: v1.ResourceType_Function,
		})
	}

	topicResources := make([]*v1.Resource, 0, len(s.Topics))
	for name := range s.Topics {
		topicResources = append(topicResources, &v1.Resource{
			Name: name,
			Type: v1.ResourceType_Topic,
		})
	}

	policies = append(policies, &v1.PolicyResource{
		Principals: principals,
		Actions: []v1.Action{
			v1.Action_TopicDetail,
			v1.Action_TopicEventPublish,
			v1.Action_TopicList,
		},
		Resources: topicResources,
	})

	bucketResources := make([]*v1.Resource, 0, len(s.Buckets))
	for name := range s.Buckets {
		bucketResources = append(bucketResources, &v1.Resource{
			Name: name,
			Type: v1.ResourceType_Bucket,
		})
	}

	policies = append(policies, &v1.PolicyResource{
		Principals: principals,
		Actions: []v1.Action{
			v1.Action_BucketFileDelete,
			v1.Action_BucketFileGet,
			v1.Action_BucketFileList,
			v1.Action_BucketFilePut,
		},
		Resources: bucketResources,
	})

	queueResources := make([]*v1.Resource, 0, len(s.Queues))
	for name := range s.Buckets {
		queueResources = append(queueResources, &v1.Resource{
			Name: name,
			Type: v1.ResourceType_Queue,
		})
	}

	policies = append(policies, &v1.PolicyResource{
		Principals: principals,
		Actions: []v1.Action{
			v1.Action_QueueDetail,
			v1.Action_QueueList,
			v1.Action_QueueReceive,
			v1.Action_QueueSend,
		},
		Resources: queueResources,
	})

	collectionResources := make([]*v1.Resource, 0, len(s.Collections))
	for name := range s.Collections {
		collectionResources = append(collectionResources, &v1.Resource{
			Name: name,
			Type: v1.ResourceType_Collection,
		})
	}

	policies = append(policies, &v1.PolicyResource{
		Principals: principals,
		Actions: []v1.Action{
			v1.Action_CollectionDocumentDelete,
			v1.Action_CollectionDocumentRead,
			v1.Action_CollectionDocumentWrite,
			v1.Action_CollectionList,
			v1.Action_CollectionQuery,
		},
		Resources: collectionResources,
	})

	secretResources := make([]*v1.Resource, 0, len(s.Secrets))
	for name := range s.Secrets {
		secretResources = append(secretResources, &v1.Resource{
			Name: name,
			Type: v1.ResourceType_Secret,
		})
	}

	policies = append(policies, &v1.PolicyResource{
		Principals: principals,
		Actions: []v1.Action{
			v1.Action_SecretAccess,
			v1.Action_SecretPut,
		},
		Resources: secretResources,
	})

	// TODO: Calculate policies for stacks loaded from a file
	return policies
}

func FromFile(name string) (*Project, error) {
	yamlFile, err := ioutil.ReadFile(name)
	if err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(path.Dir(name))
	if err != nil {
		return nil, err
	}
	stack := &Project{Dir: dir}
	err = yaml.Unmarshal(yamlFile, stack)
	if err != nil {
		return nil, err
	}
	for name, fn := range stack.Functions {
		fn.Name = name
		stack.Functions[name] = fn
	}
	for name, c := range stack.Containers {
		c.Name = name
		stack.Containers[name] = c
	}

	// Attempt to populate documents from api file references
	for k, v := range stack.Apis {
		if doc, err := openapi3.NewLoader().LoadFromFile(filepath.Join(stack.Dir, v)); err != nil {
			return nil, err
		} else {
			if stack.ApiDocs == nil {
				stack.ApiDocs = make(map[string]*openapi3.T)
			}

			stack.ApiDocs[k] = doc
		}
	}

	// Calculate default policies
	stack.Policies = calculateDefaultPolicies(stack)

	return stack, nil
}

func (s *Project) ToFile(file string) error {
	b, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(file, b, 0644)
	if err != nil {
		return err
	}

	for apiName, apiFile := range s.Apis {
		apiPath := filepath.Join(s.Dir, apiFile)
		doc, ok := s.ApiDocs[apiName]
		if !ok {
			return fmt.Errorf("apiDoc %s does not exist", apiPath)
		}
		docJ, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		err = ioutil.WriteFile(apiPath, docJ, 0644)
		if err != nil {
			return err
		}
	}
	return nil
}
