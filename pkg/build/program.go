package build

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/pearls/pkg/tui/view"
)

type Args struct {
	Project *project.Project
}

type Model struct {
	Project        *project.Project
	logMap         map[LogID][]byte
	logMultiplexer *Multiplexer
	Err            error
}

// func (m Model) Init() tea.Cmd {
// 	return tea.Batch(
// 		m.stopwatch.Init(),
// 		subscribeToChannel(m.sub),
// 		func() tea.Msg { < --
// 			return buildFunction(m.project, m.sub)
// 		})
// }

// func buildFunction(proj *project.Project, sub chan tea.Msg) tea.Msg { < ---
// 	err := build.BaseImages(proj, nil)
// 	if err != nil {
// 		return ErrorMessage{Error: err}
// 	}

// 	return FunctionsBuiltMessage{}
// }

//BaseImages(s *project.Project, logger *Multiplexer)

func (m Model) BuildExecutionUnits() tea.Msg {
	fmt.Println("building exec units")
	err := BaseImages(m.Project, m.logMultiplexer)
	if err != nil {
		return BuildErrorMsg{Err: err}
	}

	// we're done
	return tea.Quit
	// TODO: FunctionsBuiltMessage{}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		// spinner.Tick,
		m.BuildExecutionUnits,
		m.logMultiplexer.Update(),
	)
}

type BuildErrorMsg struct {
	Err error
}

// Building Images
// hello.ts... (3m2s)
//    # 4/27: RUN ncc build .
// bye.ts... (1m2s)
//	  # 16/27: COPY . .

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	fmt.Printf("got message %T\n", msg)

	switch t := msg.(type) {
	case LogMessage:
		m.logMap[t.Id] = append(m.logMap[t.Id], t.Bytes...)
		return m, m.logMultiplexer.Update()
		// case spinner.TickMsg:
		// 	m.spinner.Update()
	case tea.KeyMsg:
		switch {
		case key.Matches(t, key.NewBinding(key.WithKeys("q", "ctrl+c"))):
			return m, tea.Quit
		}
	case BuildErrorMsg:
		fmt.Println("got build error")
		m.Err = t.Err
		return m, tea.Quit
	}

	return m, nil
}

func (m Model) View() string {
	buildView := view.New()
	buildView.AddRow(view.NewFragment("Building Images"))
	// for id, bytes := range m.logMap {
	// 	buildView.AddRow(
	// 		view.NewFragment(fmt.Sprintf("%s:\n%s", id, string(bytes))),
	// 	)
	// }
	if m.Err != nil {
		errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000"))
		errorOutput := errorStyle.Render(fmt.Sprintf("eRrOr (no hoses held here): %s\n", m.Err.Error()))
		buildView.AddRow(view.NewFragment(errorOutput))
		for id, bytes := range m.logMap {
			view.NewFragment(fmt.Sprintf("Errors for: %s\n", id))
			for _, line := range strings.Split(string(bytes), "\n") {
				if strings.Contains(strings.ToLower(line), "error") {
					buildView.AddRow(
						view.NewFragment(errorStyle.Render(fmt.Sprintf("%s\n", line))),
					)
					continue
				}

			}

		}
	}
	return buildView.Render()
}

func New(args Args) Model {
	logMultiplexer := NewLogMultiplexer()
	m := Model{
		logMap:         make(map[string][]byte),
		logMultiplexer: &logMultiplexer,
		Project:        args.Project,
	}

	return m
}
