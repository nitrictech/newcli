package treetable

import (
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nitrictech/cli/pkg/tui"
)

type Model[T any] struct {
	tree Tree[T]
}

type ModelArgs[T any] struct {
	Tree Tree[T]
}

func (m Model[any]) Init() tea.Cmd {
	return nil
}

var (
	textGrayStyle = lipgloss.NewStyle().Foreground(tui.Colors.Gray)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(tui.Colors.White)
)

// Take the root node and destructure its public fields to get column names
func renderColumns[T any](tree ...Tree[T]) []table.Column {
	return []table.Column{}
}

// TODO: Externalise renderer and provide simple default renderer
func renderRow[T any](depth int, node *Node[T]) table.Row {
	return table.Row{
		// Render the left-most column with a margin that matches the depth
		lipgloss.NewStyle().SetString("├─").MarginLeft(depth).Render(),
	}
}

func renderRows[T any](depth int, nodes ...*Node[T]) []table.Row {
	rows := []table.Row{}
	for _, node := range nodes {
		rows = append(
			rows,
			renderRow(depth, node),
		)

		if len(node.Children) > 0 {
			rows = append(
				rows,
				renderRows(depth+1, node.Children...)...,
			)
		}
	}

	return rows
}

func (m Model[any]) View() string {
	table := table.New(
		table.WithColumns(renderColumns(m.tree)),
		table.WithRows(renderRows(0, m.tree.Root)),
	)

	return table.View()
}

func New[T any](args ModelArgs[T]) Model[T] {
	return Model[T]{
		tree: args.Tree,
	}
}

// TODO: May not need realtime interaction and be a display component only
// func (m Model[any]) Update(msg tea.Msg) (Model[any], tea.Cmd) {
// 	switch msg := msg.(type) {
// 	case tea.KeyMsg:

// 	}

// 	return m, nil
// }
