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
	"log"
	"net"
	"regexp"
	osruntime "runtime"
	"strings"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/imdario/mergo"
	"github.com/moby/moby/pkg/stdcopy"
	"github.com/pkg/errors"
	"github.com/pterm/pterm"
	"google.golang.org/grpc"

	"github.com/nitrictech/cli/pkg/build"
	"github.com/nitrictech/cli/pkg/containerengine"
	"github.com/nitrictech/cli/pkg/cron"
	"github.com/nitrictech/cli/pkg/output"
	"github.com/nitrictech/cli/pkg/runtime"
	"github.com/nitrictech/cli/pkg/stack"
	"github.com/nitrictech/cli/pkg/utils"
	v1 "github.com/nitrictech/nitric/pkg/api/nitric/v1"
)

// CodeConfig - represents a collection of related functions and their shared dependencies.
type CodeConfig interface {
	Collect() error
	ToStack() (*stack.Stack, error)
}

type codeConfig struct {
	// A stack can be composed of one or more applications
	functions    map[string]*FunctionDependencies
	initialStack *stack.Stack
	lock         sync.RWMutex
}

func New(s *stack.Stack) (CodeConfig, error) {
	return &codeConfig{
		initialStack: s,
		functions:    map[string]*FunctionDependencies{},
		lock:         sync.RWMutex{},
	}, nil
}

func Populate(initial *stack.Stack) (*stack.Stack, error) {
	cc, err := New(initial)
	if err != nil {
		return nil, err
	}

	err = build.CreateBaseDev(initial)
	if err != nil {
		return nil, err
	}

	err = cc.Collect()
	if err != nil {
		return nil, err
	}

	return cc.ToStack()
}

// Collect - Collects information about all functions for a nitric stack
func (c *codeConfig) Collect() error {
	wg := sync.WaitGroup{}
	errList := utils.NewErrorList()

	for _, f := range c.initialStack.Functions {
		wg.Add(1)

		// run files in parallel
		go func(fn stack.Function) {
			defer wg.Done()
			rel, err := fn.RelativeHandlerPath(c.initialStack)
			if err != nil {
				errList.Add(err)
				return
			}

			err = c.collectOne(rel)
			if err != nil {
				errList.Add(err)
				return
			}
		}(f)
	}

	wg.Wait()

	return errList.Aggregate()
}

type apiHandler struct {
	worker *v1.ApiWorker
	target string
}

var alphanumeric, _ = regexp.Compile("[^a-zA-Z0-9]+")

// apiSpec produces an open api v3 spec for the requests API name
func (c *codeConfig) apiSpec(api string) (*openapi3.T, error) {
	doc := &openapi3.T{
		Paths: make(openapi3.Paths),
	}

	doc.Info = &openapi3.Info{
		Title:   api,
		Version: "v1",
	}

	doc.OpenAPI = "3.0.1"

	// Compile an API specification from the functions in the stack for the given API name
	workers := make([]*apiHandler, 0)

	// Collect all workers
	for handler, f := range c.functions {
		rt, err := runtime.NewRunTimeFromHandler(handler)
		if err != nil {
			return nil, err
		}
		if f.apis[api] != nil {
			for _, w := range f.apis[api].workers {
				workers = append(workers, &apiHandler{
					target: rt.ContainerName(),
					worker: w,
				})
			}
		}
	}

	// loop over workers to build new api specification
	// FIXME: We will need to merge path matches across all workers
	// to ensure we don't have conflicts
	for _, w := range workers {
		params := make(openapi3.Parameters, 0)
		normalizedPath := ""
		for _, p := range strings.Split(w.worker.Path, "/") {
			if strings.HasPrefix(p, ":") {
				paramName := strings.Replace(p, ":", "", -1)
				params = append(params, &openapi3.ParameterRef{
					Value: &openapi3.Parameter{
						In:       "path",
						Name:     paramName,
						Required: true,
						Schema: &openapi3.SchemaRef{
							Value: &openapi3.Schema{
								Type: "string",
							},
						},
					},
				})
				normalizedPath = normalizedPath + "{" + paramName + "}" + "/"
			} else {
				normalizedPath = normalizedPath + p + "/"
			}
		}
		// trim off trailing slash
		normalizedPath = strings.TrimSuffix(normalizedPath, "/")

		pathItem := doc.Paths.Find(normalizedPath)

		if pathItem == nil {
			// Add the parameters at the path level
			pathItem = &openapi3.PathItem{
				Parameters: params,
			}
			// Add the path item to the document
			doc.Paths[normalizedPath] = pathItem
		}

		for _, m := range w.worker.Methods {
			if pathItem.Operations() != nil && pathItem.Operations()[m] != nil {
				// If the operation already exists we should fail
				// NOTE: This should not happen as operations are stored in a map
				// in the api state for functions
				return nil, fmt.Errorf("found conflicting operations")
			}

			doc.AddOperation(normalizedPath, m, &openapi3.Operation{
				OperationID: strings.ToLower(alphanumeric.ReplaceAllString(normalizedPath+m, "")),
				Responses:   openapi3.NewResponses(),
				ExtensionProps: openapi3.ExtensionProps{
					Extensions: map[string]interface{}{
						"x-nitric-target": map[string]string{
							"type": "function",
							"name": w.target,
						},
					},
				},
			})
		}
	}

	return doc, nil
}

// collectOne - Collects information about a function for a nitric stack
// handler - the specific handler for the application
func (c *codeConfig) collectOne(handler string) error {
	rt, err := runtime.NewRunTimeFromHandler(handler)
	if err != nil {
		return errors.WithMessage(err, "error getting the runtime from handler "+handler)
	}

	name := rt.ContainerName()
	fun := NewFunction(name)

	srv := NewServer(name, fun)
	grpcSrv := grpc.NewServer()

	v1.RegisterResourceServiceServer(grpcSrv, srv)
	v1.RegisterFaasServiceServer(grpcSrv, srv)

	lis, err := net.Listen("tcp", ":0")
	if err != nil {
		return err
	}

	port := lis.Addr().(*net.TCPAddr).Port

	defer lis.Close()

	errChan := make(chan error)
	go func(errChan chan error) {
		errChan <- grpcSrv.Serve(lis)
	}(errChan)

	// run the handler in a container
	// Specify the service bind as the port with the docker gateway IP (running in bridge mode)
	ce, err := containerengine.Discover()
	if err != nil {
		return errors.WithMessage(err, "error discovering container engine")
	}

	opts, err := rt.LaunchOptsForFunctionCollect(c.initialStack.Dir)
	if err != nil {
		return err
	}

	hostConfig := &container.HostConfig{
		AutoRemove: true,
		Mounts:     opts.Mounts,
	}
	if osruntime.GOOS == "linux" {
		// setup host.docker.internal to route to host gateway
		// to access rpc server hosted by local CLI run
		hostConfig.ExtraHosts = []string{"host.docker.internal:172.17.0.1"}
	}

	cc := &container.Config{
		AttachStdout: true,
		AttachStderr: true,
		Image:        opts.Image,
		Env: []string{
			fmt.Sprintf("SERVICE_ADDRESS=host.docker.internal:%d", port),
			fmt.Sprintf("NITRIC_SERVICE_PORT=%d", port),
			fmt.Sprintf("NITRIC_SERVICE_HOST=%s", "host.docker.internal"),
			"NITRIC_ENVIRONMENT=build", // this is to tell the sdk that we are running in the build and not proper runtime.
		},
		Cmd:        opts.Cmd,
		Entrypoint: opts.Entrypoint,
		WorkingDir: opts.TargetWD,
	}

	cID, err := ce.ContainerCreate(cc, hostConfig, nil, rt.ContainerName())
	if err != nil {
		return err
	}

	err = ce.Start(cID)
	if err != nil {
		return err
	}

	pterm.Debug.Println(containerengine.Cli(cc, hostConfig))
	if output.VerboseLevel > 1 {
		logreader, err := ce.ContainerLogs(cID, types.ContainerLogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Follow:     true,
		})
		if err != nil {
			return err
		}
		go func() {
			_, _ = stdcopy.StdCopy(log.Writer(), log.Writer(), logreader)
		}()
	}

	errs := utils.NewErrorList().WithSubject(handler)
	waitChan, cErrChan := ce.ContainerWait(cID, container.WaitConditionNextExit)
	select {
	case done := <-waitChan:
		msg := ""
		if done.Error != nil {
			msg = done.Error.Message
		}
		if msg != "" || done.StatusCode != 0 {
			errs.Add(fmt.Errorf("error executing in container (code %d) %s", done.StatusCode, msg))
		}
	case cErr := <-cErrChan:
		errs.Add(cErr)
	}

	// When the container exits stop the server
	grpcSrv.Stop()
	errs.Add(<-errChan)

	// Add the function
	c.addFunction(fun, handler)
	return errs.Aggregate()
}

func (c *codeConfig) addFunction(fun *FunctionDependencies, handler string) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.functions[handler] = fun
}

func (c *codeConfig) ToStack() (*stack.Stack, error) {
	s := stack.New(c.initialStack.Name, c.initialStack.Dir)

	err := mergo.Merge(s, c.initialStack)
	if err != nil {
		return nil, err
	}

	errs := utils.NewErrorList()
	for handler, f := range c.functions {
		topicTriggers := make([]string, 0, len(f.subscriptions)+len(f.schedules))

		for k := range f.apis {
			spec, err := c.apiSpec(k)
			if err != nil {
				return nil, fmt.Errorf("could not build spec for api: %s; %w", k, err)
			}

			s.ApiDocs[k] = spec
		}
		for k := range f.buckets {
			s.Buckets[k] = stack.Bucket{}
		}
		for k := range f.collections {
			s.Collections[k] = stack.Collection{}
		}
		for k := range f.queues {
			s.Queues[k] = stack.Queue{}
		}
		for k := range f.secrets {
			s.Secrets[k] = stack.Secret{}
		}

		// Add policies
		s.Policies = append(s.Policies, f.policies...)

		for k, v := range f.schedules {
			// Create a new topic target
			// replace spaced with hyphens
			topicName := strings.ToLower(strings.ReplaceAll(k, " ", "-"))
			s.Topics[topicName] = stack.Topic{}

			topicTriggers = append(topicTriggers, topicName)

			var exp string
			if v.GetCron() != nil {
				exp = v.GetCron().Cron
			} else if v.GetRate() != nil {
				e, err := cron.RateToCron(v.GetRate().Rate)

				if err != nil {
					errs.Add(fmt.Errorf("schedule expresson %s is invalid; %w", v.GetRate().Rate, err))
					continue
				}

				exp = e
			} else {
				errs.Add(fmt.Errorf("schedule %s is invalid", v.String()))
				continue
			}

			newS := stack.Schedule{
				Expression: exp,
				Target: stack.ScheduleTarget{
					Type: "topic",
					Name: topicName,
				},
				Event: stack.ScheduleEvent{
					PayloadType: "io.nitric.schedule",
					Payload: map[string]interface{}{
						"schedule": k,
					},
				},
			}
			if current, ok := s.Schedules[k]; ok {
				if err := mergo.Merge(&current, &newS); err != nil {
					errs.Add(err)
				} else {
					s.Schedules[k] = current
				}
			} else {
				s.Schedules[k] = newS
			}
		}

		for k := range f.topics {
			s.Topics[k] = stack.Topic{}
		}

		for k := range f.subscriptions {
			if _, ok := f.topics[k]; !ok {
				errs.Add(fmt.Errorf("subscription to topic %s defined, but topic does not exist", k))
			} else {
				topicTriggers = append(topicTriggers, k)
			}
		}

		fun, ok := s.Functions[f.name]
		if !ok {
			fun = stack.FunctionFromHandler(handler, s.Dir)
		}
		fun.ComputeUnit.Triggers = stack.Triggers{
			Topics: topicTriggers,
		}
		s.Functions[f.name] = fun
	}

	return s, errs.Aggregate()
}
