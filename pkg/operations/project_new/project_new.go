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

package project_new

import (
	"fmt"
	"path"
	"path/filepath"
	"time"

	"github.com/spf13/afero"

	"github.com/charmbracelet/bubbles/spinner"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/goombaio/namegenerator"

	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/cli/pkg/templates"
	"github.com/nitrictech/cli/pkg/utils"
	"github.com/nitrictech/pearls/pkg/tui"
	"github.com/nitrictech/pearls/pkg/tui/listprompt"
	"github.com/nitrictech/pearls/pkg/tui/textprompt"
	"github.com/nitrictech/pearls/pkg/tui/validation"
	"github.com/nitrictech/pearls/pkg/tui/view"
)

type (
	errMsg error
)

type NewProjectStatus int

const (
	NameInput NewProjectStatus = iota
	TemplateInput
	Pending
	Done
	Error
)

// Model - represents the state of the new project creation operation
type Model struct {
	namePrompt     textprompt.Model
	templatePrompt listprompt.Model
	spinner        spinner.Model
	status         NewProjectStatus
	nonInteractive bool
	fs             afero.Fs

	err error
}

// ProjectName - returns the project name entered by the user
func (m Model) ProjectName() string {
	return m.namePrompt.Value()
}

// TemplateName returns the project template name selected by the user
func (m Model) TemplateName() string {
	return m.templatePrompt.Choice()
}

// Init initializes the model, used by Bubbletea
func (m Model) Init() tea.Cmd {
	if m.err != nil {
		return tea.Quit
	}

	if m.nonInteractive {
		return tea.Batch(m.spinner.Tick, m.createProject())
	}

	return tea.Batch(tea.ClearScreen, m.namePrompt.Init(), m.templatePrompt.Init())
}

// Update the model based on a message
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		}

	case projectCreateResultMsg:
		if msg.err == nil {
			m.status = Done
		} else {
			m.status = Error
			m.err = msg.err
		}

		return m, nil

	case errMsg:
		m.err = msg
		return m, tea.Quit
	case textprompt.CompleteMsg:
		if msg.ID == m.namePrompt.ID {
			m.namePrompt.Blur()
			m.status = TemplateInput
		}

		return m, nil
	}

	// Deal with the various steps in the process from data capture to building the project
	switch m.status {
	case NameInput:
		m.namePrompt, cmd = m.namePrompt.UpdateTextPrompt(msg)
	case TemplateInput:
		m.templatePrompt, cmd = m.templatePrompt.UpdateListPrompt(msg)
		if m.templatePrompt.Choice() != "" {
			m.status = Pending
			return m, tea.Batch(m.spinner.Tick, m.createProject())
		}
	case Pending:
		m.spinner, cmd = m.spinner.Update(msg)
	case Done:
		return m, tea.Quit
	}

	return m, cmd
}

var (
	titleStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Foreground(tui.Colors.White).
			Background(tui.Colors.Blue).
			MarginRight(2)
	spinnerStyle   = lipgloss.NewStyle().Foreground(tui.Colors.Purple)
	highlightStyle = lipgloss.NewStyle().Foreground(tui.Colors.Purple)
)

var (
	errorTagStyle           = lipgloss.NewStyle().Background(tui.Colors.Red).Foreground(tui.Colors.White).PaddingLeft(2).PaddingRight(2).Align(lipgloss.Center)
	errorTextStyle          = lipgloss.NewStyle().PaddingLeft(2).Foreground(tui.Colors.Red)
	tagStyle                = lipgloss.NewStyle().Width(8).Background(tui.Colors.Purple).Foreground(tui.Colors.White).Align(lipgloss.Center)
	projCreatedHeadingStyle = lipgloss.NewStyle().Bold(true).MarginLeft(2)
)

func (m Model) View() string {
	projectView := view.New()

	if m.err != nil {
		projectView.AddRow(
			view.NewFragment("error").WithStyle(errorTagStyle),
			view.NewFragment(m.err.Error()).WithStyle(errorTextStyle),
			// TODO: this shouldn't be needed but without it the line doesn't print
			view.Break(),
		)

		return projectView.Render()
	}

	if !m.nonInteractive {
		projectView.AddRow(
			view.NewFragment("nitric").WithStyle(titleStyle),
			view.NewFragment("Let's get going!"),
			view.Break(),
		)

		projectView.AddRow(
			view.NewFragment(m.namePrompt.View()),
		)

		// Template selection input
		if m.status >= TemplateInput {
			projectView.AddRow(
				view.NewFragment(m.templatePrompt.View()),
			)
		}
	}

	// Creating Status
	if m.status == Pending {
		projectView.AddRow(
			view.Break(),
			view.NewFragment("proj").WithStyle(tagStyle),
			view.NewFragment(m.spinner.View()).WithStyle(lipgloss.NewStyle().MarginLeft(2)),
			view.NewFragment(" creating project..."),
			view.Break(),
		)
	}

	// Done!
	if m.status == Done {
		projectView.AddRow(
			view.Break(),
			view.NewFragment("proj").WithStyle(tagStyle),
			view.NewFragment("Project Created!").WithStyle(projCreatedHeadingStyle),
			view.Break(),
		)

		shiftRight := lipgloss.NewStyle().MarginLeft(10)

		projectView.AddRow(
			view.NewFragment("Navigate to your project with "),
			view.NewFragment(fmt.Sprintf("cd ./%s", m.ProjectName())).WithStyle(highlightStyle),
		).WithStyle(shiftRight)

		projectView.AddRow(
			view.NewFragment("Install dependencies and you're ready to rock! 🪨"),
			view.Break(),
		).WithStyle(shiftRight)

		projectView.AddRow(
			view.NewFragment("Need help? Come and chat "),
			view.NewFragment("https://nitric.io/chat").WithStyle(highlightStyle),
			view.Break(),
		).WithStyle(shiftRight)
	} else {
		projectView.AddRow(
			view.Break(),
			view.NewFragment("(esc to quit)").WithStyle(lipgloss.NewStyle().Foreground(tui.Colors.Gray)),
		)
	}

	return projectView.Render()
}

type Args struct {
	ProjectName  string
	TemplateName string
}

func New(fs afero.Fs, args Args) Model {
	seed := time.Now().UTC().UnixNano()
	nameGenerator := namegenerator.NewNameGenerator(seed)
	placeholderName := nameGenerator.Generate()

	nameValidator := validation.ComposeValidators(projectNameValidators...)
	nameInFlightValidator := validation.ComposeValidators(projectNameInFlightValidators...)

	namePrompt := textprompt.NewTextPrompt("projectName", textprompt.TextPromptArgs{
		Prompt:            "What should we name this project?",
		Tag:               "name",
		Placeholder:       placeholderName,
		Validator:         nameValidator,
		InFlightValidator: nameInFlightValidator,
	})
	namePrompt.Focus()

	downloadr := templates.NewDownloader()
	templateNames, err := downloadr.Names()
	utils.CheckErr(err)

	templatePrompt := listprompt.New(listprompt.Args{
		Prompt:            "Which template should we start with?",
		Tag:               "tmpl",
		Items:             templateNames,
		MaxDisplayedItems: len(templateNames),
	})

	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = spinnerStyle

	// prefill values from CLI args
	if args.ProjectName != "" {
		if err := nameValidator(args.ProjectName); err != nil {
			return Model{
				err: err,
			}
		}

		namePrompt.SetValue(args.ProjectName)
	}

	if args.TemplateName != "" {
		if downloadr.Get(args.TemplateName) == nil {
			return Model{
				err: fmt.Errorf("template \"%s\" could not be found", args.TemplateName),
			}
		}

		templatePrompt.SetChoice(args.TemplateName)
	}

	isNonInteractive := false
	projectStatus := NameInput

	if args.ProjectName != "" {
		projectStatus = TemplateInput
	}

	if args.TemplateName != "" {
		isNonInteractive = true
		projectStatus = Pending
	}

	return Model{
		namePrompt:     namePrompt,
		templatePrompt: templatePrompt,
		nonInteractive: isNonInteractive,
		status:         projectStatus,
		spinner:        s,
		err:            nil,
	}
}

type projectCreateResultMsg struct {
	err error
}

// createProject returns a command that will create the project on disk using the inputs gathered
func (m Model) createProject() tea.Cmd {
	return func() tea.Msg {
		cd, err := filepath.Abs(".")
		utils.CheckErr(err)

		projDir := path.Join(cd, m.ProjectName())

		downloadr := templates.NewDownloader()
		err = downloadr.DownloadDirectoryContents(m.TemplateName(), projDir, false)
		utils.CheckErr(err)

		var p *project.Config

		// Load and update the project name in the template's nitric.yaml
		p, err = project.ConfigFromProjectPath(m.fs, projDir)
		utils.CheckErr(err)

		p.Name = m.ProjectName()

		return projectCreateResultMsg{err: p.ToFile()}
	}
}
