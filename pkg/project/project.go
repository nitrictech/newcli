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
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/samber/lo"
	"github.com/spf13/afero"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	goruntime "runtime"

	"github.com/nitrictech/cli/pkg/cloud"
	"github.com/nitrictech/cli/pkg/collector"
	"github.com/nitrictech/cli/pkg/preview"
	"github.com/nitrictech/cli/pkg/project/runtime"
	"github.com/nitrictech/nitric/core/pkg/logger"
)

const tempBuildDir = "./.nitric/build"

// BuildImage - Builds the docker image for the service

type Project struct {
	Name      string
	Directory string
	Preview   []preview.Feature

	services []Service
	batches  []Batch
}

func (p *Project) GetServices() []Service {
	return p.services
}

// TODO: Reduce duplicate code
// BuildBatches - Builds all the batches in the project
func (p *Project) BuildBatches(fs afero.Fs) (chan ServiceBuildUpdate, error) {
	updatesChan := make(chan ServiceBuildUpdate)

	if len(p.services) == 0 {
		return nil, fmt.Errorf("no services found in project, nothing to build. This may indicate misconfigured `match` patterns in your nitric.yaml file")
	}

	maxConcurrentBuilds := make(chan struct{}, min(goruntime.NumCPU(), goruntime.GOMAXPROCS(0)))

	waitGroup := sync.WaitGroup{}

	for _, batch := range p.batches {
		waitGroup.Add(1)
		// Create writer
		serviceBuildUpdateWriter := &serviceBuildUpdateWriter{
			buildUpdateChan: updatesChan,
			serviceName:     batch.Name,
		}

		go func(svc Batch, writer io.Writer) {
			// Acquire a token by filling the maxConcurrentBuilds channel
			// this will block once the buffer is full
			maxConcurrentBuilds <- struct{}{}

			// Start goroutine
			if err := svc.BuildImage(fs, writer); err != nil {
				updatesChan <- ServiceBuildUpdate{
					ServiceName: svc.Name,
					Err:         err,
					Message:     err.Error(),
					Status:      ServiceBuildStatus_Error,
				}
			} else {
				updatesChan <- ServiceBuildUpdate{
					ServiceName: svc.Name,
					Message:     "Build Complete",
					Status:      ServiceBuildStatus_Complete,
				}
			}

			// release our lock
			<-maxConcurrentBuilds

			waitGroup.Done()
		}(batch, serviceBuildUpdateWriter)
	}

	go func() {
		waitGroup.Wait()
		// Drain the semaphore to make sure all goroutines have finished
		for i := 0; i < cap(maxConcurrentBuilds); i++ {
			maxConcurrentBuilds <- struct{}{}
		}

		close(updatesChan)
	}()

	return updatesChan, nil
}

// BuildServices - Builds all the services in the project
func (p *Project) BuildServices(fs afero.Fs) (chan ServiceBuildUpdate, error) {
	updatesChan := make(chan ServiceBuildUpdate)

	if len(p.services) == 0 {
		return nil, fmt.Errorf("no services found in project, nothing to build. This may indicate misconfigured `match` patterns in your nitric.yaml file")
	}

	maxConcurrentBuilds := make(chan struct{}, min(goruntime.NumCPU(), goruntime.GOMAXPROCS(0)))

	waitGroup := sync.WaitGroup{}

	for _, service := range p.services {
		waitGroup.Add(1)
		// Create writer
		serviceBuildUpdateWriter := &serviceBuildUpdateWriter{
			buildUpdateChan: updatesChan,
			serviceName:     service.Name,
		}

		go func(svc Service, writer io.Writer) {
			// Acquire a token by filling the maxConcurrentBuilds channel
			// this will block once the buffer is full
			maxConcurrentBuilds <- struct{}{}

			// Start goroutine
			if err := svc.BuildImage(fs, writer); err != nil {
				updatesChan <- ServiceBuildUpdate{
					ServiceName: svc.Name,
					Err:         err,
					Message:     err.Error(),
					Status:      ServiceBuildStatus_Error,
				}
			} else {
				updatesChan <- ServiceBuildUpdate{
					ServiceName: svc.Name,
					Message:     "Build Complete",
					Status:      ServiceBuildStatus_Complete,
				}
			}

			// release our lock
			<-maxConcurrentBuilds

			waitGroup.Done()
		}(service, serviceBuildUpdateWriter)
	}

	go func() {
		waitGroup.Wait()
		// Drain the semaphore to make sure all goroutines have finished
		for i := 0; i < cap(maxConcurrentBuilds); i++ {
			maxConcurrentBuilds <- struct{}{}
		}

		close(updatesChan)
	}()

	return updatesChan, nil
}

func (p *Project) collectServiceRequirements(service Service) (*collector.ServiceRequirements, error) {
	serviceRequirements := collector.NewServiceRequirements(service.Name, service.GetFilePath(), service.Type)

	// start a grpc service with this registered
	grpcServer := grpc.NewServer()

	serviceRequirements.RegisterServices(grpcServer)

	listener, err := net.Listen("tcp", ":")
	if err != nil {
		return nil, err
	}

	// register non-blocking
	go func() {
		err := grpcServer.Serve(listener)
		if err != nil {
			logger.Errorf("unable to start local Nitric collection server: %s", err)
		}
	}()

	defer grpcServer.Stop()

	// run the service we want to collect for targeting the grpc server
	// TODO: load and run .env files, etc.
	stopChannel := make(chan bool)
	updatesChannel := make(chan ServiceRunUpdate)

	go func() {
		// TODO: elevate env for tmp diretory and reuse
		tmpCollectDir := "./.nitric/collect"

		err := os.MkdirAll(tmpCollectDir, os.ModePerm)
		if err != nil {
			log.Fatalf("unable to create collect log directory %s", err)
		}

		// Create a temporary log file for this service
		logFile, err := afero.TempFile(afero.NewOsFs(), tmpCollectDir, fmt.Sprintf("nitric-%s-*.log", service.Name))
		if err != nil {
			log.Fatalf("unable to create collect log file: %s", err)
		}

		defer logFile.Close()

		for update := range updatesChannel {
			_, err = logFile.WriteString(update.Message)
			if err != nil {
				log.Fatalf("unable to write update log %s", err)
			}

			if update.Err != nil {
				_, err = logFile.WriteString(update.Err.Error())
				if err != nil {
					log.Fatalf("unable to write update error log %s", err)
				}
			}
		}
	}()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return nil, fmt.Errorf("unable to split host and port for local Nitric collection server: %w", err)
	}

	err = service.RunContainer(stopChannel, updatesChannel, WithNitricPort(port), WithNitricEnvironment("build"))
	if err != nil {
		return nil, err
	}

	if serviceRequirements.HasDatabases() && !slices.Contains(p.Preview, preview.Feature_SqlDatabases) {
		return nil, fmt.Errorf("service %s requires a database, but the project does not have the 'sql-databases' preview feature enabled. Please add sql-databases to the preview field of your nitric.yaml file to enable this feature", service.filepath)
	}

	return serviceRequirements, nil
}

func (p *Project) collectBatchRequirements(service Batch) (*collector.BatchRequirements, error) {
	serviceRequirements := collector.NewBatchRequirements(service.Name, service.GetFilePath())

	// start a grpc service with this registered
	grpcServer := grpc.NewServer()

	serviceRequirements.RegisterServices(grpcServer)

	listener, err := net.Listen("tcp", ":")
	if err != nil {
		return nil, err
	}

	// register non-blocking
	go func() {
		err := grpcServer.Serve(listener)
		if err != nil {
			logger.Errorf("unable to start local Nitric collection server: %s", err)
		}
	}()

	defer grpcServer.Stop()

	// run the service we want to collect for targeting the grpc server
	// TODO: load and run .env files, etc.
	stopChannel := make(chan bool)
	updatesChannel := make(chan ServiceRunUpdate)

	go func() {
		for range updatesChannel {
			// TODO: Provide some updates - bubbletea nice output
			// fmt.Println("container update:", update)
			continue
		}
	}()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return nil, fmt.Errorf("unable to split host and port for local Nitric collection server: %w", err)
	}

	err = service.RunContainer(stopChannel, updatesChannel, WithNitricPort(port), WithNitricEnvironment("build"))
	if err != nil {
		return nil, err
	}

	if serviceRequirements.HasDatabases() && !slices.Contains(p.Preview, preview.Feature_SqlDatabases) {
		return nil, fmt.Errorf("service %s requires a database, but the project does not have the 'sql-databases' preview feature enabled. Please add sql-databases to the preview field of your nitric.yaml file to enable this feature", service.filepath)
	}

	return serviceRequirements, nil
}

func (p *Project) CollectServicesRequirements() ([]*collector.ServiceRequirements, error) {
	allServiceRequirements := []*collector.ServiceRequirements{}
	serviceErrors := []error{}

	reqLock := sync.Mutex{}
	errorLock := sync.Mutex{}
	wg := sync.WaitGroup{}

	for _, service := range p.services {
		svc := service

		wg.Add(1)

		go func(s Service) {
			defer wg.Done()

			serviceRequirements, err := p.collectServiceRequirements(s)
			if err != nil {
				errorLock.Lock()
				defer errorLock.Unlock()

				serviceErrors = append(serviceErrors, err)

				return
			}

			reqLock.Lock()
			defer reqLock.Unlock()

			allServiceRequirements = append(allServiceRequirements, serviceRequirements)
		}(svc)
	}

	wg.Wait()

	if len(serviceErrors) > 0 {
		return nil, errors.Join(serviceErrors...)
	}

	return allServiceRequirements, nil
}

func (p *Project) CollectBatchRequirements() ([]*collector.BatchRequirements, error) {
	allBatchRequirements := []*collector.BatchRequirements{}
	batchErrors := []error{}

	reqLock := sync.Mutex{}
	errorLock := sync.Mutex{}
	wg := sync.WaitGroup{}

	for _, batch := range p.batches {
		b := batch

		wg.Add(1)

		go func(s Batch) {
			defer wg.Done()

			batchRequirements, err := p.collectBatchRequirements(s)
			if err != nil {
				errorLock.Lock()
				defer errorLock.Unlock()

				batchErrors = append(batchErrors, err)

				return
			}

			reqLock.Lock()
			defer reqLock.Unlock()

			allBatchRequirements = append(allBatchRequirements, batchRequirements)
		}(b)
	}

	wg.Wait()

	if len(batchErrors) > 0 {
		return nil, errors.Join(batchErrors...)
	}

	return allBatchRequirements, nil
}

// DefaultMigrationImage - Returns the default migration image name for the project
// Also returns ok if image is required or not
func (p *Project) DefaultMigrationImage(fs afero.Fs) (string, bool) {
	ok, _ := afero.DirExists(fs, "./migrations")

	return fmt.Sprintf("%s-nitric-migrations", p.Name), ok
}

// RunServices - Runs all the services locally using a startup command
// use the stop channel to stop all running services
func (p *Project) RunServicesWithCommand(localCloud *cloud.LocalCloud, stop <-chan bool, updates chan<- ServiceRunUpdate, env map[string]string) error {
	stopChannels := lo.FanOut[bool](len(p.services), 1, stop)

	group, _ := errgroup.WithContext(context.TODO())

	for i, service := range p.services {
		idx := i
		svc := service

		// start the service with the given file reference from its projects CWD
		group.Go(func() error {
			port, err := localCloud.AddService(svc.filepath)
			if err != nil {
				return err
			}

			envVariables := map[string]string{
				"PYTHONUNBUFFERED":   "TRUE", // ensure all print statements print immediately for python
				"NITRIC_ENVIRONMENT": "run",
				"SERVICE_ADDRESS":    "localhost:" + strconv.Itoa(port),
			}

			for key, value := range env {
				envVariables[key] = value
			}

			return svc.Run(stopChannels[idx], updates, envVariables)
		})
	}

	return group.Wait()
}

// RunServices - Runs all the services as containers
// use the stop channel to stop all running services
func (p *Project) RunServices(localCloud *cloud.LocalCloud, stop <-chan bool, updates chan<- ServiceRunUpdate, env map[string]string) error {
	stopChannels := lo.FanOut[bool](len(p.services), 1, stop)

	group, _ := errgroup.WithContext(context.TODO())

	for i, service := range p.services {
		idx := i
		svc := service

		group.Go(func() error {
			port, err := localCloud.AddService(svc.filepath)
			if err != nil {
				return err
			}

			return svc.RunContainer(stopChannels[idx], updates, WithNitricPort(strconv.Itoa(port)), WithEnvVars(env))
		})
	}

	return group.Wait()
}

func (pc *ProjectConfiguration) pathToNormalizedServiceName(servicePath string) string {
	// Add the project name as a prefix to group service images
	servicePath = fmt.Sprintf("%s_%s", pc.Name, servicePath)
	// replace path separators with dashes
	servicePath = strings.ReplaceAll(servicePath, string(os.PathSeparator), "-")
	// remove the file extension
	servicePath = strings.ReplaceAll(servicePath, filepath.Ext(servicePath), "")
	// replace dots with dashes
	servicePath = strings.ReplaceAll(servicePath, ".", "-")
	// replace all non-word characters
	servicePath = strings.ReplaceAll(servicePath, "[^\\w]", "-")

	return strings.ToLower(servicePath)
}

// fromProjectConfiguration creates a new Instance of a nitric Project from a configuration files contents
func fromProjectConfiguration(projectConfig *ProjectConfiguration, fs afero.Fs) (*Project, error) {
	services := []Service{}
	batches := []Batch{}

	matches := map[string]string{}

	fmt.Printf("Project Config: %+v\n", projectConfig)

	for _, serviceSpec := range projectConfig.Services {
		files, err := afero.Glob(fs, serviceSpec.Match)
		if err != nil {
			return nil, fmt.Errorf("unable to match service files for pattern %s: %w", serviceSpec.Match, err)
		}

		for _, f := range files {
			relativeServiceEntrypointPath, _ := filepath.Rel(projectConfig.Directory, f)

			serviceName := projectConfig.pathToNormalizedServiceName(relativeServiceEntrypointPath)

			var buildContext *runtime.RuntimeBuildContext

			otherEntryPointFiles := lo.Filter(files, func(file string, index int) bool {
				return file != f
			})

			if serviceSpec.Runtime != "" {
				// We have a custom runtime
				customRuntime, ok := projectConfig.Runtimes[serviceSpec.Runtime]
				if !ok {
					return nil, fmt.Errorf("unable to find runtime %s", serviceSpec.Runtime)
				}

				buildContext, err = runtime.NewBuildContext(
					relativeServiceEntrypointPath,
					customRuntime.Dockerfile,
					customRuntime.Context, // will default to the project directory if not set
					customRuntime.Args,
					otherEntryPointFiles,
					fs,
				)
				if err != nil {
					return nil, fmt.Errorf("unable to create build context for custom service file %s: %w", f, err)
				}
			} else {
				buildContext, err = runtime.NewBuildContext(
					relativeServiceEntrypointPath,
					"",
					".",
					map[string]string{},
					otherEntryPointFiles,
					fs,
				)
				if err != nil {
					return nil, fmt.Errorf("unable to create build context for service file %s: %w", f, err)
				}
			}

			if matches[f] != "" {
				return nil, fmt.Errorf("service file %s matched by multiple patterns: %s and %s, services must only be matched by a single pattern", f, matches[f], serviceSpec.Match)
			}

			matches[f] = serviceSpec.Match

			newService := Service{
				Name:         serviceName,
				filepath:     f,
				buildContext: *buildContext,
				Type:         serviceSpec.Type,
				startCmd:     serviceSpec.Start,
			}

			if serviceSpec.Type == "" {
				serviceSpec.Type = "default"
			}

			services = append(services, newService)
		}
	}

	for _, batchSpec := range projectConfig.Batches {
		files, err := afero.Glob(fs, batchSpec.Match)
		if err != nil {
			return nil, fmt.Errorf("unable to match batch files for pattern %s: %w", batchSpec.Match, err)
		}

		for _, f := range files {
			relativeServiceEntrypointPath, _ := filepath.Rel(projectConfig.Directory, f)

			serviceName := projectConfig.pathToNormalizedServiceName(relativeServiceEntrypointPath)

			var buildContext *runtime.RuntimeBuildContext

			otherEntryPointFiles := lo.Filter(files, func(file string, index int) bool {
				return file != f
			})

			if batchSpec.Runtime != "" {
				// We have a custom runtime
				customRuntime, ok := projectConfig.Runtimes[batchSpec.Runtime]
				if !ok {
					return nil, fmt.Errorf("unable to find runtime %s", batchSpec.Runtime)
				}

				buildContext, err = runtime.NewBuildContext(
					relativeServiceEntrypointPath,
					customRuntime.Dockerfile,
					customRuntime.Args,
					otherEntryPointFiles,
					fs,
				)
				if err != nil {
					return nil, fmt.Errorf("unable to create build context for custom service file %s: %w", f, err)
				}
			} else {
				buildContext, err = runtime.NewBuildContext(
					relativeServiceEntrypointPath,
					"",
					map[string]string{},
					otherEntryPointFiles,
					fs,
				)
				if err != nil {
					return nil, fmt.Errorf("unable to create build context for batch file %s: %w", f, err)
				}
			}

			if matches[f] != "" {
				return nil, fmt.Errorf("batch file %s matched by multiple patterns: %s and %s, batches must only be matched by a single pattern", f, matches[f], batchSpec.Match)
			}

			matches[f] = batchSpec.Match

			newBatch := Batch{
				Name:         serviceName,
				filepath:     f,
				buildContext: *buildContext,
				runCmd:       batchSpec.Run,
			}

			batches = append(batches, newBatch)
		}
	}

	return &Project{
		Name:      projectConfig.Name,
		Directory: projectConfig.Directory,
		Preview:   projectConfig.Preview,
		services:  services,
		batches:   batches,
	}, nil
}

// FromFile - Loads a nitric project from a nitric.yaml file
// If no filepath is provided, the default location './nitric.yaml' is used
func FromFile(fs afero.Fs, filepath string) (*Project, error) {
	projectConfig, err := ConfigurationFromFile(fs, filepath)
	if err != nil {
		return nil, fmt.Errorf("unable to load nitric.yaml, are you currently in a nitric project?: %w", err)
	}

	return fromProjectConfiguration(projectConfig, fs)
}
