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
	"bytes"
	"fmt"
	"log"
	"net"
	"os"
	"regexp"
	osruntime "runtime"
	"strings"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/imdario/mergo"
	multierror "github.com/missionMeteora/toolkit/errors"
	"github.com/moby/moby/pkg/stdcopy"
	"github.com/pkg/errors"
	"github.com/pterm/pterm"
	"google.golang.org/grpc"

	"github.com/nitrictech/cli/pkg/build"
	"github.com/nitrictech/cli/pkg/containerengine"
	"github.com/nitrictech/cli/pkg/cron"
	"github.com/nitrictech/cli/pkg/output"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/runtime"
	"github.com/nitrictech/cli/pkg/utils"
	pb "github.com/nitrictech/nitric/pkg/api/nitric/v1"
	v1 "github.com/nitrictech/nitric/pkg/api/nitric/v1"
)

// CodeConfig - represents a collection of related functions and their shared dependencies.
type CodeConfig interface {
	Collect() error
	ToProject() (*project.Project, error)
}

type codeConfig struct {
	// A stack can be composed of one or more applications
	functions      map[string]*FunctionDependencies
	initialProject *project.Project
	envMap         map[string]string
	lock           sync.RWMutex
}

func New(p *project.Project, envMap map[string]string) (CodeConfig, error) {
	return &codeConfig{
		initialProject: p,
		functions:      map[string]*FunctionDependencies{},
		lock:           sync.RWMutex{},
		envMap:         envMap,
	}, nil
}

func Populate(initial *project.Project, envMap map[string]string) (*project.Project, error) {
	cc, err := New(initial, envMap)
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

	return cc.ToProject()
}

func (c *codeConfig) Collect() error {
	wg := sync.WaitGroup{}
	errList := &multierror.ErrorList{}

	for _, f := range c.initialProject.Functions {
		wg.Add(1)

		// run files in parallel
		go func(fn project.Function) {
			defer wg.Done()
			rel, err := fn.RelativeHandlerPath(c.initialProject)
			if err != nil {
				errList.Push(err)
				return
			}

			err = c.collectOne(rel)
			if err != nil {
				errList.Push(err)
				return
			}
		}(f)
	}

	wg.Wait()

	return errList.Err()
}

type apiHandler struct {
	worker *v1.ApiWorker
	target string
}

var alphanumeric, _ = regexp.Compile("[^a-zA-Z0-9]+")

// Get the security definitions for an API in this stack
func (c *codeConfig) securityDefinitions(api string) (map[string]*pb.ApiSecurityDefinition, error) {
	sds := make(map[string]*pb.ApiSecurityDefinition)
	for _, f := range c.functions {
		// TODO: Ensure the function actually has API definitions
		if f.apis != nil && f.apis[api] != nil && f.apis[api].securityDefinitions != nil {
			for sn, sd := range f.apis[api].securityDefinitions {
				// TODO: Check if this security definition has already been defined for conflicts
				sds[sn] = sd
			}
		}
	}

	return sds, nil
}

// apiSpec produces an open api v3 spec for the requests API name
func (c *codeConfig) apiSpec(api string) (*openapi3.T, error) {
	doc := &openapi3.T{
		Paths: make(openapi3.Paths),
		Info: &openapi3.Info{
			Title:   api,
			Version: "v1",
		},
		OpenAPI: "3.0.1",
	}

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

			// Apply top level security rules to the API
			if len(f.apis[api].security) > 0 {
				for n, scopes := range f.apis[api].security {
					doc.Security.With(openapi3.SecurityRequirement{
						n: scopes,
					})
				}
			}
		}
	}

	// loop over workers to build new api specification
	// FIXME: We will need to merge path matches across all workers
	// to ensure we don't have conflicts
	for _, w := range workers {
		normalizedPath, params := splitPath(w.worker.Path)
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

			exts := map[string]interface{}{
				"x-nitric-target": map[string]string{
					"type": "function",
					"name": w.target,
				},
			}

			var sr *openapi3.SecurityRequirements = nil
			if w.worker.Options != nil {
				if w.worker.Options.SecurityDisabled {
					sr = &openapi3.SecurityRequirements{}
				} else if len(w.worker.Options.Security) > 0 {
					sr = &openapi3.SecurityRequirements{}
					if !w.worker.Options.SecurityDisabled {
						for key, scopes := range w.worker.Options.Security {
							sr.With(openapi3.SecurityRequirement{
								key: scopes.Scopes,
							})
						}
					}
				}
			}

			pathItem.SetOperation(m, &openapi3.Operation{
				OperationID: strings.ToLower(alphanumeric.ReplaceAllString(normalizedPath+m, "")),
				Responses:   openapi3.NewResponses(),
				ExtensionProps: openapi3.ExtensionProps{
					Extensions: exts,
				},
				Security: sr,
			})
		}
	}

	if output.VerboseLevel > 3 {
		b, err := doc.MarshalJSON()
		if err != nil {
			return nil, err
		}
		fmt.Println("discovered api doc", string(b))
	}
	return doc, nil
}

func ensureOneTrailingSlash(p string) string {
	if len(p) > 0 && string(p[len(p)-1]) == "/" {
		return p
	}
	return p + "/"
}

func splitPath(workerPath string) (string, openapi3.Parameters) {
	normalizedPath := ""

	params := make(openapi3.Parameters, 0)
	for _, p := range strings.Split(workerPath, "/") {
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
			normalizedPath = ensureOneTrailingSlash(normalizedPath + "{" + paramName + "}")
		} else {
			normalizedPath = ensureOneTrailingSlash(normalizedPath + p)
		}
	}
	// trim off trailing slash
	if normalizedPath != "/" {
		normalizedPath = strings.TrimSuffix(normalizedPath, "/")
	}
	return normalizedPath, params
}

func useHostInterface(hc *container.HostConfig, iface string, port int) ([]string, error) {
	dockerInternalAddr, err := utils.GetInterfaceIpv4Addr(iface)
	if err != nil {
		return nil, err
	}
	fmt.Println("dockerInternalAddr ", dockerInternalAddr)

	hc.NetworkMode = "host"

	return []string{
		fmt.Sprintf("SERVICE_ADDRESS=%s:%d", dockerInternalAddr, port),
		fmt.Sprintf("NITRIC_SERVICE_PORT=%d", port),
		fmt.Sprintf("NITRIC_SERVICE_HOST=%s", dockerInternalAddr),
	}, nil
}

func useDockerInternal(hc *container.HostConfig, port int) []string {
	if osruntime.GOOS == "linux" {
		// setup host.docker.internal to route to host gateway
		// to access rpc server hosted by local CLI run
		hc.ExtraHosts = []string{"host.docker.internal:172.17.0.1"}
	}

	return []string{
		fmt.Sprintf("SERVICE_ADDRESS=%s:%d", "host.docker.internal", port),
		fmt.Sprintf("NITRIC_SERVICE_PORT=%d", port),
		fmt.Sprintf("NITRIC_SERVICE_HOST=%s", "host.docker.internal"),
	}
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

	opts, err := rt.LaunchOptsForFunctionCollect(c.initialProject.Dir)
	if err != nil {
		return err
	}

	hostConfig := &container.HostConfig{
		AutoRemove: true,
		Mounts:     opts.Mounts,
	}

	var env []string
	if os.Getenv("HOST_DOCKER_INTERNAL_IFACE") != "" {
		env, err = useHostInterface(hostConfig, os.Getenv("HOST_DOCKER_INTERNAL_IFACE"), port)
	} else {
		env = useDockerInternal(hostConfig, port)
	}
	if err != nil {
		return err
	}

	// this is to tell the sdk that we are running in the build and not proper runtime.
	env = append(env, "NITRIC_ENVIRONMENT=build")

	for k, v := range c.envMap {
		env = append(env, k+"="+v)
	}

	cc := &container.Config{
		AttachStdout: true,
		AttachStderr: true,
		Image:        opts.Image,
		Env:          env,
		Cmd:          opts.Cmd,
		Entrypoint:   opts.Entrypoint,
		WorkingDir:   opts.TargetWD,
	}

	if output.VerboseLevel > 2 {
		pterm.Debug.Println(containerengine.Cli(cc, hostConfig))
	}

	cn := strings.Join([]string{c.initialProject.Name, "codeAsConfig", rt.ContainerName()}, "-")
	cID, err := ce.ContainerCreate(cc, hostConfig, &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{},
	}, cn)
	if err != nil {
		return err
	}

	err = ce.Start(cID)
	if err != nil {
		return err
	}

	logreader, err := ce.ContainerLogs(cID, types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return err
	}

	logWriter := log.Writer()
	logRW := &bytes.Buffer{}
	if output.VerboseLevel <= 1 {
		// if we are running in non-verbose then store the container logs in a buffer in case
		// there are errors.
		logWriter = logRW
	}
	go func() {
		_, _ = stdcopy.StdCopy(logWriter, logWriter, logreader)
	}()

	errs := multierror.ErrorList{}
	waitChan, cErrChan := ce.ContainerWait(cID, container.WaitConditionNextExit)
	select {
	case done := <-waitChan:
		msg := ""
		if done.Error != nil {
			msg = done.Error.Message
		}
		if logRW.Len() > 0 {
			for {
				line, err := logRW.ReadString('\n')
				if err != nil {
					break
				}
				msg += "\n" + line
			}
		}
		if done.StatusCode != 0 {
			errs.Push(fmt.Errorf("error executing in container (code %d) %s", done.StatusCode, msg))
		}
	case cErr := <-cErrChan:
		errs.Push(cErr)
	}

	// When the container exits stop the server
	grpcSrv.Stop()
	cErr := <-errChan
	if cErr != nil {
		errs.Push(cErr)
	}

	// Add the function
	c.addFunction(fun, handler)
	return errs.Err()
}

func (c *codeConfig) addFunction(fun *FunctionDependencies, handler string) {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.functions[handler] = fun
}

func (c *codeConfig) ToProject() (*project.Project, error) {
	s := project.New(&project.Config{Name: c.initialProject.Name, Dir: c.initialProject.Dir})

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
					Type: "function",
					Name: f.name,
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
			fun, err = project.FunctionFromHandler(handler, s.Dir)
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
