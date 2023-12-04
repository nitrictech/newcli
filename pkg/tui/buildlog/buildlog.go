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

package buildlog

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/stopwatch"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/samber/lo"

	"github.com/nitrictech/cli/pkg/build"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/pearls/pkg/tui"
	"github.com/nitrictech/pearls/pkg/tui/view"
)

const tagWidth = 10

var (
	textStyle     = lipgloss.NewStyle().Foreground(tui.Colors.White).Align(lipgloss.Left)
	labelStyle    = lipgloss.NewStyle().Inherit(textStyle)
	ignoreStyle   = lipgloss.NewStyle().Foreground(tui.Colors.Gray)
	textAreaStyle = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), true, false)
	tagStyle      = lipgloss.NewStyle().Foreground(tui.Colors.White).Width(tagWidth).Align(lipgloss.Center).Bold(true)
)

type Model struct {
	sub chan tea.Msg

	stopwatch stopwatch.Model
	help      help.Model
	viewport  viewport.Model

	envs    map[string]string
	project *project.Project

	functionColors map[string]lipgloss.CompleteColor
	functionLogs   map[string]*LogWriter
	logs           []LogMessage

	Complete      bool
	viewportReady bool
	manualScroll  bool
}

type ModelArgs struct {
	Envs    map[string]string
	Project *project.Project
	Sub     chan tea.Msg
}

func New(args ModelArgs) Model {
	functionColors := make(map[string]lipgloss.CompleteColor, 0)
	functionLogs := make(map[string]*LogWriter, 0)

	for idx, fun := range lo.Values(args.Project.Functions) {
		functionLogs[fun.Name] = &LogWriter{
			Sub:      args.Sub,
			Function: fun.Name,
		}

		functionColors[fun.Name] = getRandomColor(idx)
	}

	return Model{
		envs:           args.Envs,
		project:        args.Project,
		stopwatch:      stopwatch.NewWithInterval(time.Second),
		sub:            args.Sub,
		functionColors: functionColors,
		functionLogs:   functionLogs,
		Complete:       false,
	}
}

func getRandomColor(seed int) lipgloss.CompleteColor {
	colours := []lipgloss.CompleteColor{
		tui.Colors.Red, tui.Colors.Yellow, tui.Colors.Teal, tui.Colors.Purple, tui.Colors.Orange, tui.Colors.Green, tui.Colors.Blue,
	}

	return colours[seed%len(colours)]
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.stopwatch.Init(),
		subscribeToChannel(m.sub),
		func() tea.Msg {
			return buildFunction(m.project, m.sub)
		})
}

func buildFunction(proj *project.Project, sub chan tea.Msg) tea.Msg {
	err := build.BaseImages(proj, nil)
	if err != nil {
		return ErrorMessage{Error: err}
	}

	return FunctionsBuiltMessage{}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, BuildLogKeys.Quit):
			return m, tea.Quit
		case key.Matches(msg, BuildLogKeys.Down):
			m.manualScroll = true
			m.viewport.LineDown(3)

			return m, nil
		case key.Matches(msg, BuildLogKeys.Up):
			m.manualScroll = true
			m.viewport.LineUp(3)

			return m, nil
		case key.Matches(msg, BuildLogKeys.Continue):
			m.manualScroll = false

			if m.viewport.Height > 0 {
				m.viewport.GotoBottom()
			}

			return m, nil
		}
	case LogMessage:
		m.logs = append(m.logs, msg)

		if !m.manualScroll && m.viewport.Height > 0 {
			m.viewport.GotoBottom()
		}

		return m, subscribeToChannel(m.sub)
	case FunctionsBuiltMessage:
		m.Complete = true
		cmd := m.stopwatch.Stop()

		return m, cmd
	case ErrorMessage:
		fmt.Println(msg.Error)
		return m, tea.Quit
	case tea.WindowSizeMsg:
		m.help.Width = msg.Width

		// the number of lines that content other than the viewport fills.
		otherContentLines := 14

		if !m.viewportReady {
			m.viewport = viewport.New(msg.Width, msg.Height-otherContentLines)
			m.viewportReady = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - otherContentLines
			if m.viewport.Height < 0 {
				m.viewport.Height = 0
			}
		}

		return m, tea.ClearScreen
	}

	m.viewport.SetContent(m.getLogView())

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	m.stopwatch, cmd = m.stopwatch.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	renderer := view.New().WithStyle(textStyle)

	if !m.Complete {
		renderer.AddRow(
			view.NewFragment("Building Images..."),
			view.Break(),
			view.Break(),
			view.NewFragment(m.viewport.View()).WithStyle(textAreaStyle),
			view.Break(),
			view.Break(),
			view.NewFragment("Elapsed: "+m.stopwatch.View()),
			view.Break(),
			view.Break(),
			view.NewFragment(m.help.FullHelpView(BuildLogKeys.FullHelp())).WithStyle(ignoreStyle),
		)
	} else {
		renderer.AddRow(
			view.NewFragment("Images built in: " + m.stopwatch.View()),
		)
	}

	return renderer.Render()
}

func (m *Model) getLogView() string {
	logRenderer := view.New()

	for _, log := range m.logs {
		logRenderer.AddRow(
			// Write tag with colour
			view.NewFragment(getTagName(log.Function)).WithStyle(
				lipgloss.NewStyle().
					Inherit(tagStyle).
					Background(m.functionColors[log.Function]).
					MarginRight(2),
			),
			// Write label
			view.WhenOr(
				log.Info,
				view.NewFragment(log.Message).WithStyle(ignoreStyle),
				view.NewFragment(log.Message).WithStyle(labelStyle),
			),
		)
	}

	return logRenderer.Render()
}

func getTagName(function string) string {
	length := len(function)

	if length > tagWidth {
		length = tagWidth
	}

	return function[:length]
}
