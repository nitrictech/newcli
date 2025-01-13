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

package services

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"

	"github.com/nitrictech/cli/pkg/cloud"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/system"
	"github.com/nitrictech/cli/pkg/view/tui"
	"github.com/nitrictech/cli/pkg/view/tui/commands/local"
	"github.com/nitrictech/cli/pkg/view/tui/components/view"
	"github.com/nitrictech/cli/pkg/view/tui/fragments"
	"github.com/nitrictech/cli/pkg/view/tui/reactive"
	"github.com/nitrictech/cli/pkg/view/tui/teax"
)

type Model struct {
	stopChan           chan<- bool
	updateChan         <-chan project.ServiceRunUpdate
	localServicesModel tea.Model

	windowSize tea.WindowSizeMsg
	viewOffset int

	serviceStatus     map[string]project.ServiceRunUpdate
	serviceRunUpdates []project.ServiceRunUpdate
}

var _ tea.Model = (*Model)(nil)

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		reactive.AwaitChannel(m.updateChan),
		m.localServicesModel.Init(),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		if msg.Type == tea.MouseWheelUp {
			m.viewOffset++
		} else if msg.Type == tea.MouseWheelDown {
			m.viewOffset = max(0, m.viewOffset-1)
		}
	case tea.WindowSizeMsg:
		m.windowSize = msg
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, tui.KeyMap.Quit):
			func() {
				m.stopChan <- true
			}()

			return m, teax.Quit
		case key.Matches(msg, tui.KeyMap.Up):
			m.viewOffset++
		case key.Matches(msg, tui.KeyMap.Down):
			m.viewOffset = max(0, m.viewOffset-1)
		}
	case reactive.ChanMsg[project.ServiceRunUpdate]:
		// we know we have a service update
		m.serviceStatus[msg.Value.ServiceName] = msg.Value
		m.serviceRunUpdates = append(m.serviceRunUpdates, msg.Value)

		logger := system.GetServiceLogger()
		// Write log to file and handle any errors
		level := logrus.InfoLevel

		if msg.Value.Status == project.ServiceRunStatus_Error {
			level = logrus.ErrorLevel
		}

		logger.WriteLog(level, msg.Value.Message, msg.Value.Label)

		return m, reactive.AwaitChannel(msg.Source)
	default:
		// give unknown messages to to sub model
		newLocalModel, cmd := m.localServicesModel.Update(msg)
		m.localServicesModel = newLocalModel

		return m, cmd
	}

	var cmd tea.Cmd
	m.localServicesModel, cmd = m.localServicesModel.Update(msg)

	return m, cmd
}

var serviceColors = []lipgloss.CompleteAdaptiveColor{
	tui.Colors.Blue,
	tui.Colors.Purple,
	tui.Colors.Teal,
	tui.Colors.Red,
	tui.Colors.Orange,
	tui.Colors.Green,
}

func tail(text string, take int, offset int) string {
	if offset < 0 {
		offset = 0
	}

	if take < 1 {
		return text
	}

	lines := strings.Split(text, "\n")
	if len(lines) < 1 {
		return ""
	}

	totalLines := len(lines)

	if offset > totalLines {
		offset = totalLines
	}

	start := lo.Max([]int{0, totalLines - take}) - offset
	if start < 0 {
		start = 0
	}

	end := lo.Min([]int{totalLines, start + take})
	if end > totalLines {
		end = totalLines
	}

	return strings.Join(lines[start:end], "\n")
}

func getMessageChunks(columnWidth int, update project.ServiceRunUpdate) []string {
	messageLines := strings.Split(strings.TrimSuffix(update.Message, "\n"), "\n")
	messageChunks := []string{}

	fileLength := len(update.Label) + 2

	firstLineWidth := columnWidth - fileLength
	if firstLineWidth < 0 {
		return []string{}
	}

	for _, message := range messageLines {
		startPoint := 0
		endPoint := firstLineWidth

		section := 0

		for endPoint < len(message) {
			messageChunks = append(messageChunks, message[startPoint:endPoint])

			section++

			startPoint = firstLineWidth * section
			if section > 1 {
				startPoint = (columnWidth * section) - fileLength
			}

			endPoint = (columnWidth * (section + 1)) - fileLength
		}

		messageChunks = append(messageChunks, message[startPoint:])
	}

	return messageChunks
}

func (m Model) View() string {
	heightStyle := lipgloss.NewStyle().MaxHeight(m.windowSize.Height - 4)
	availableWidth := m.windowSize.Width - 10 // 5 for borders and padding, 5 for safe output when the program exits.
	leftWidth := availableWidth / 3
	rightWidth := availableWidth - leftWidth

	// TODO: lipgloss width wrapping breaks with long text using dashes.
	lv := view.New(view.WithStyle(heightStyle.Copy().Width(leftWidth)))
	rv := view.New(view.WithStyle(lipgloss.NewStyle().Width(rightWidth)))

	if len(m.serviceStatus) == 0 {
		lv.Addln("No service found in project, check your nitric.yaml file contains at least one valid 'match' pattern.")
	} else {
		lv.Addf("%d", len(m.serviceStatus)).WithStyle(lipgloss.NewStyle().Bold(true).Foreground(tui.Colors.TextHighlight))
		lv.Addln(" services registered with local nitric server")
	}

	svcColors := map[string]lipgloss.CompleteAdaptiveColor{}
	serviceNames := lo.Keys(m.serviceStatus)

	slices.Sort(serviceNames)

	for idx, svcName := range serviceNames {
		svcColors[svcName] = serviceColors[idx%len(serviceColors)]
	}

	for _, update := range m.serviceRunUpdates {
		statusColor := tui.Colors.TextMuted
		if update.Status == project.ServiceRunStatus(project.ServiceBuildStatus_Error) {
			statusColor = tui.Colors.Red
		}

		rv.Addf("%s: ", update.Label).WithStyle(lipgloss.NewStyle().Foreground(svcColors[update.ServiceName]))

		// Break the message into multiple lines so the foreground colour can be maintained

		messageChunks := getMessageChunks(rightWidth, update)
		for _, chunk := range messageChunks {
			// we'll inject our own newline, so remove the duplicate suffix. Retain any other newlines intended by the user
			rv.Add(chunk).WithStyle(lipgloss.NewStyle().Foreground(statusColor))
			rv.Break()
		}
	}

	lv.Addln(m.localServicesModel.View())

	rightRaw := rv.Render()
	rightBorder := lipgloss.NewStyle().BorderForeground(tui.Colors.Gray).Border(lipgloss.NormalBorder(), false, false, false, true).PaddingLeft(1).MarginLeft(1)

	finalRightView := view.New(view.WithStyle(rightBorder))
	finalRightView.Add(tail(rightRaw, m.windowSize.Height-5, m.viewOffset))

	sideBySide := lipgloss.JoinHorizontal(lipgloss.Top, lv.Render(), finalRightView.Render())

	return lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(tui.Colors.Gray).Render(sideBySide) + "\n " + fragments.Hotkey("esc", "quit") + " " + fragments.Hotkey("↑/↓", "navigate logs")
}

func NewModel(stopChannel chan<- bool, updateChannel <-chan project.ServiceRunUpdate, localCloud *cloud.LocalCloud, dashboardUrl string) Model {
	localServicesModel := local.NewTuiModel(localCloud, dashboardUrl)

	return Model{
		stopChan:           stopChannel,
		localServicesModel: localServicesModel,
		updateChan:         updateChannel,
		serviceStatus:      make(map[string]project.ServiceRunUpdate),
		serviceRunUpdates:  []project.ServiceRunUpdate{},
		viewOffset:         0,
	}
}
