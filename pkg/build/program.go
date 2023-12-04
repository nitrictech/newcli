package build

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nitrictech/cli/pkg/project"
	"github.com/nitrictech/pearls/pkg/tui/view"
)

type Args struct {
	Functions []*project.Function
}

type Model struct {
}

func (m Model) Init() tea.Cmd {
	return tea.Batch()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	fmt.Printf("got message %T\n", msg)

	switch t := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(t, key.NewBinding(key.WithKeys("q", "ctrl+c"))):
			return m, tea.Quit
		}
	}

	return m, nil
}

// TODO: Tim's brain says this might be bad, super, duper, bad! (due to a timeout buried in termenv)
var statusStyle = lipgloss.NewStyle().Foreground(view.)

func (m Model) View() string {
	buildView := view.New()
	buildView.AddRow(view.NewFragment("Building Images"))
	for _, fun := range m.Functions {
		status := "Building" // TODO: get this from somewhere
		statusFrag := view.NewFragment(status)
		buildView.AddRow(view.NewFragment(fmt.Sprintf("  %s", fun.Name)))

	}
	return buildView.Render()
}

func New(args Args) Model {
	// logMultiplexer := NewLogMultiplexer()
	m := Model{}

	return m
}

func BuildFunctions() {

}
