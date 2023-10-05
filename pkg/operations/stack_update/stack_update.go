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

package stack_update

import (
	"fmt"
	"log"
	"os"
	"regexp"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/joho/godotenv"
	"github.com/pterm/pterm"

	"github.com/nitrictech/cli/pkg/build"
	"github.com/nitrictech/cli/pkg/codeconfig"
	"github.com/nitrictech/cli/pkg/output"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/provider"
	"github.com/nitrictech/cli/pkg/provider/types"
	"github.com/nitrictech/cli/pkg/stack"
	"github.com/nitrictech/cli/pkg/tasklet"
	"github.com/nitrictech/cli/pkg/utils"
)

// Model - represents the state of the new project creation operation
type Model struct {
	viewPort viewport.Model

	content string

	updates chan types.Event

	ready bool
}

var clearControlChars = regexp.MustCompile(`\x1B\[2J|\x1B\[K|\x1B\[1K|\x1B\[0K`)

// Init initializes the model, used by Bubbletea
func (m Model) Init() tea.Cmd {
	// if m.nonInteractive {
	// 	return tea.Batch(m.spinner.Tick, m.createProject())
	// }

	// return tea.Batch(tea.ClearScreen, m.namePrompt.Init(), m.templatePrompt.Init())
	// TODO Initialize
	return tea.Batch(
		m.subscribeToUpdates(),
		tea.EnterAltScreen,
		// m.viewPort.Init(),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	// case tea.KeyMsg:
	// 	if k := msg.String(); k == "ctrl+c" || k == "q" || k == "esc" {
	// 		return m, tea.Quit
	// 	}

	case types.Event:
		if msg.Progress != nil {
			m.content = clearControlChars.ReplaceAllString(msg.Progress.Content, "")
			m.viewPort.SetContent(m.content)
		}
		cmds = append(cmds, m.subscribeToUpdates())
	case tea.WindowSizeMsg:
		// headerHeight := lipgloss.Height(m.headerView())
		// footerHeight := lipgloss.Height(m.footerView())
		// verticalMarginHeight := headerHeight + footerHeight

		if !m.ready {
			// Since this program is using the full size of the viewport we
			// need to wait until we've received the window dimensions before
			// we can initialize the viewport. The initial dimensions come in
			// quickly, though asynchronously, which is why we wait for them
			// here.
			m.viewPort = viewport.New(msg.Width, msg.Height)
			// m.viewPort.YPosition = headerHeight
			// m.viewPort.HighPerformanceRendering = useHighPerformanceRenderer
			// m.viewPort.SetContent("")
			m.viewPort.SetContent(m.content)

			m.ready = true

			// This is only necessary for high performance rendering, which in
			// most cases you won't need.
			//
			// Render the viewport one line below the header.
			// m.viewport.YPosition = headerHeight + 1
		} else {
			m.viewPort.Width = msg.Width
			m.viewPort.Height = msg.Height
		}

		// if useHighPerformanceRenderer {
		// 	// Render (or re-render) the whole viewport. Necessary both to
		// 	// initialize the viewport and when the window is resized.
		// 	//
		// 	// This is needed for high-performance rendering only.
		// 	cmds = append(cmds, viewport.Sync(m.viewport))
		// }
	}

	// Handle keyboard and mouse events in the viewport
	m.viewPort, cmd = m.viewPort.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	if !m.ready {
		return "\n  Initializing..."
	}
	return fmt.Sprintf("Running Deploy: \n %s", m.viewPort.View())
	// return m.content
	//  "Running Deploy..."
}

type Args struct {
	EnvFile     string
	Stack       *stack.Config
	Force       bool
	Interactive bool
}

func (m Model) subscribeToUpdates() tea.Cmd {
	return func() tea.Msg {
		return <-m.updates
	}
}

func Run(args Args) {
	config, err := project.ConfigFromProjectPath("")
	utils.CheckErr(err)

	proj, err := project.FromConfig(config)
	utils.CheckErr(err)

	log.SetOutput(output.NewPtermWriter(pterm.Debug))
	log.SetFlags(0)

	envFiles := utils.FilesExisting(".env", ".env.production", args.EnvFile)

	envMap := map[string]string{}

	if len(envFiles) > 0 {
		envMap, err = godotenv.Read(envFiles...)
		utils.CheckErr(err)
	}

	// build base images on updates
	createBaseImage := tasklet.Runner{
		StartMsg: "Building Images",
		Runner: func(_ output.Progress) error {
			return build.BuildBaseImages(proj)
		},
		StopMsg: "Images Built",
	}
	tasklet.MustRun(createBaseImage, tasklet.Opts{})

	cc, err := codeconfig.New(proj, envMap)
	utils.CheckErr(err)

	codeAsConfig := tasklet.Runner{
		StartMsg: "Gathering configuration from code..",
		Runner: func(_ output.Progress) error {
			return cc.Collect()
		},
		StopMsg: "Configuration gathered",
	}

	tasklet.MustRun(codeAsConfig, tasklet.Opts{})

	p, err := provider.ProviderFromFile(cc, args.Stack.Name, args.Stack.Provider, envMap, &types.ProviderOpts{Force: args.Force, Interactive: args.Interactive})
	utils.CheckErr(err)

	// Run the tea program
	updateChan := make(chan types.Event)
	program := tea.NewProgram(Model{
		updates: updateChan,
	}, tea.WithANSICompressor())

	go program.Run()
	defer program.Quit()

	d := &types.Deployment{}
	p.Subscribe(updateChan)
	d, err = p.Up()

	if err != nil {
		os.Exit(1)
	}
	// deploy := tasklet.Runner{
	// 	StartMsg: "Deploying..",
	// 	Runner: func(progress output.Progress) error {

	// 	},
	// 	StopMsg: "Stack",
	// }
	// tasklet.MustRun(deploy, tasklet.Opts{SuccessPrefix: "Deployed"})

	// Print callable APIs if any were deployed
	if len(d.ApiEndpoints) > 0 {
		rows := [][]string{{"API", "Endpoint"}}
		for k, v := range d.ApiEndpoints {
			rows = append(rows, []string{k, v})
		}

		_ = pterm.DefaultTable.WithBoxed().WithData(rows).Render()
	}
}
