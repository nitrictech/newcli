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

package local_run

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bep/debounce"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/pkg/errors"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"

	"github.com/nitrictech/cli/pkg/build"
	"github.com/nitrictech/cli/pkg/containerengine"
	"github.com/nitrictech/cli/pkg/dashboard"
	"github.com/nitrictech/cli/pkg/env"
	"github.com/nitrictech/cli/pkg/output"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/run"
	"github.com/nitrictech/cli/pkg/tasklet"
	"github.com/nitrictech/cli/pkg/tui/buildlog"
	"github.com/nitrictech/cli/pkg/tui/localservices"
	"github.com/nitrictech/cli/pkg/utils"
	"github.com/nitrictech/pearls/pkg/tui/view"
)

type State int

const (
	BuildingImages State = iota
	StartingLocalServices
	RunningFunctions
)

type Model struct {
	state         State
	buildLog      buildlog.Model
	localServices localservices.Model
	sub           chan tea.Msg
}

func (m Model) nextState() (Model, tea.Cmd) {
	switch m.state {
	case BuildingImages:
		m.state = StartingLocalServices
		return m, m.localServices.Init()
	case StartingLocalServices:
		m.state = RunningFunctions
		return m, nil
	case RunningFunctions:
		return m, tea.Quit
	}

	return m, nil
}

func (m Model) Init() tea.Cmd {
	return m.buildLog.Init()
}

func (m Model) View() string {
	renderer := view.New()

	renderer.AddRow(
		view.NewFragment(m.buildLog.View()),
		view.Break(),
		view.Break(),
		view.NewFragment(m.localServices.View()),
	)

	return renderer.Render()
}

func subscribeToChannel(sub chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-sub
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	var cmds []tea.Cmd

	switch m.state {
	case BuildingImages:
		bl, cmd := m.buildLog.Update(msg)
		m.buildLog = bl.(buildlog.Model)

		cmds = append(cmds, cmd)
	case StartingLocalServices, RunningFunctions:
		ls, cmd := m.localServices.Update(msg)
		m.localServices = ls.(localservices.Model)

		cmds = append(cmds, cmd)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("q", "ctrl+c"))):
			return m, tea.Quit
		}
	case buildlog.FunctionsBuiltMessage, localservices.FunctionsStartedMessage:
		m, cmd = m.nextState()
		cmds = append(cmds, cmd, subscribeToChannel(m.sub))

		return m, tea.Batch(cmds...)
	}

	return m, tea.Batch(cmds...)
}

type ModelArgs struct {
	NoBrowser      bool
	NonInteractive bool
}

func New(ctx context.Context, args ModelArgs) Model {
	proj, err := getProjectConfig()
	utils.CheckErr(err)

	envMap, err := env.LoadFromDefaultFiles()
	utils.CheckErr(err)

	ce, err := containerengine.Discover()
	utils.CheckErr(err)

	logger := ce.Logger(proj.Dir)
	utils.CheckErr(logger.Start())

	sub := make(chan tea.Msg)

	buildLog := buildlog.New(buildlog.ModelArgs{
		Envs:    envMap,
		Project: proj,
		Sub:     sub,
	})

	localServices := localservices.New(localservices.ModelArgs{
		Envs:      envMap,
		Project:   proj,
		Sub:       sub,
		NoBrowser: args.NoBrowser,
	})

	m := Model{
		buildLog:      buildLog,
		localServices: localServices,
		state:         BuildingImages,
	}

	return m
}

func RunNonInteractive(noBrowser bool) error {
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM, syscall.SIGINT)

	config, err := project.ConfigFromProjectPath("")
	utils.CheckErr(err)

	proj, err := project.FromConfig(config)
	utils.CheckErr(err)

	envMap, err := env.LoadFromDefaultFiles()
	utils.CheckErr(err)

	dash, err := dashboard.New(proj, envMap)
	if err != nil {
		utils.CheckErr(err)
	}

	ls := run.NewLocalServices(proj, false, dash)
	if ls.Running() {
		pterm.Error.Println("Only one instance of Nitric can be run locally at a time, please check that you have ended all other instances and try again")
		os.Exit(2)
	}

	ce, err := containerengine.Discover()
	utils.CheckErr(err)

	logger := ce.Logger(proj.Dir)
	utils.CheckErr(logger.Start())

	createBaseImage := tasklet.Runner{
		StartMsg: "Building Images",
		Runner: func(_ output.Progress) error {
			return build.BaseImages(proj, nil)
		},
		StopMsg: "Images Built",
	}
	tasklet.MustRun(createBaseImage, tasklet.Opts{Signal: term})

	memerr := make(chan error)
	pool := run.NewRunProcessPool()

	startLocalServices := tasklet.Runner{
		StartMsg: "Starting local services",
		Runner: func(progress output.Progress) error {
			go func(errch chan error) {
				errch <- ls.Start(pool, false)
			}(memerr)

			for {
				select {
				case err := <-memerr:
					// catch any early errors from Start()
					if err != nil {
						return err
					}
				default:
				}
				if ls.Running() {
					break
				}
				progress.Busyf("Waiting for local services...")
				time.Sleep(time.Second)
			}
			return nil
		},
		StopMsg: "Local services running",
	}
	tasklet.MustRun(startLocalServices, tasklet.Opts{
		Signal: term,
	})

	var functions []*run.Function

	startFunctions := tasklet.Runner{
		StartMsg: "Starting functions",
		Runner: func(_ output.Progress) error {
			functions, err = run.FunctionsFromHandlers(proj)
			if err != nil {
				return err
			}
			for _, f := range functions {
				err = f.Start(envMap)
				if err != nil {
					return err
				}
			}
			return nil
		},
		StopMsg: "Functions running",
	}
	tasklet.MustRun(startFunctions, tasklet.Opts{Signal: term})

	pterm.DefaultBasicText.Println("Application running, use ctrl-C to stop")

	stackState := run.NewStackState(proj)

	err = ls.Refresh()
	if err != nil {
		utils.CheckErr(err)
	}

	// Start local dashboard
	err = dash.Serve(ls.GetStorageService(), true)
	utils.CheckErr(err)

	stackState.Update(pool, ls)

	area, _ := pterm.DefaultArea.Start()
	area.Update(stackState.Tables())

	// Create a debouncer for the refresh and remove locking
	debounced := debounce.New(500 * time.Millisecond)

	// React to worker pool state and update services table
	pool.Listen(func(we run.WorkerEvent) {
		debounced(func() {
			err := ls.Refresh()
			if err != nil {
				cobra.CheckErr(err)
			}

			stackState.Update(pool, ls)

			area.Update(stackState.Tables())

			for _, warning := range stackState.Warnings() {
				pterm.Warning.Println(warning)
			}
		})
	})

	select {
	case membraneError := <-memerr:
		fmt.Println(errors.WithMessage(membraneError, "membrane error, exiting"))
	case <-term:
		fmt.Println("Shutting down services - exiting")
	}

	for _, f := range functions {
		if err = f.Stop(); err != nil {
			fmt.Println(f.Name(), " stop error ", err)
		}
	}

	_ = area.Stop()
	_ = logger.Stop()

	// Stop the membrane
	return ls.Stop()
}

func getProjectConfig() (*project.Project, error) {
	config, err := project.ConfigFromProjectPath("")
	if err != nil {
		return nil, err
	}

	proj, err := project.FromConfig(config)
	if err != nil {
		return nil, err
	}

	return proj, err
}
