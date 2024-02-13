package stack_up

import (
	"fmt"
	"time"

	_ "embed"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/lipgloss"
	"github.com/samber/lo"

	tui "github.com/nitrictech/cli/pkgplus/view/tui"
	"github.com/nitrictech/cli/pkgplus/view/tui/commands/stack"
	"github.com/nitrictech/cli/pkgplus/view/tui/components/view"
	"github.com/nitrictech/cli/pkgplus/view/tui/fragments"
	"github.com/nitrictech/cli/pkgplus/view/tui/reactive"
	"github.com/nitrictech/cli/pkgplus/view/tui/teax"
	deploymentspb "github.com/nitrictech/nitric/core/pkg/proto/deployments/v1"
)

type Model struct {
	provider           string
	stack              *stack.Resource
	defaultParent      *stack.Resource
	updatesChan        <-chan *deploymentspb.DeploymentUpEvent
	errorChan          <-chan error
	providerStdoutChan <-chan string
	providerStdout     []string
	errs               []error
	resultOutput       string

	done bool

	windowSize tea.WindowSizeMsg

	spinner spinner.Model
}

var _ tea.Model = Model{}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		reactive.AwaitChannel(m.updatesChan),
		reactive.AwaitChannel(m.errorChan),
		reactive.AwaitChannel(m.providerStdoutChan),
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.windowSize = msg
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.done = true
			return m, teax.Quit
		}

	case reactive.ChanMsg[string]:
		if !msg.Ok {
			break
		}

		m.providerStdout = append(m.providerStdout, msg.Value)

		return m, reactive.AwaitChannel(msg.Source)
	case reactive.ChanMsg[*deploymentspb.DeploymentUpEvent]:

		// the source channel is closed
		if !msg.Ok {
			m.done = true
			return m, teax.Quit
		}

		switch content := msg.Value.Content.(type) {
		case *deploymentspb.DeploymentUpEvent_Update:

			if content.Update == nil || content.Update.Id == nil {
				break
			}

			name := content.Update.SubResource
			if name == "" {
				name = fmt.Sprintf("%s::%s", content.Update.Id.Type.String(), content.Update.Id.Name)
			}

			parent := m.stack
			if content.Update.SubResource != "" {
				nitricResource, found := lo.Find(m.stack.Children, func(r *stack.Resource) bool {
					return r.Name == fmt.Sprintf("%s::%s", content.Update.Id.Type.String(), content.Update.Id.Name)
				})

				if found {
					parent = nitricResource
				} else {
					// add to the default container, used for resources that are stack level, but not explicitly defined.
					parent = m.defaultParent
				}
			}

			existingChild, found := lo.Find(parent.Children, func(item *stack.Resource) bool {
				return item.Name == name
			})

			now := time.Now()

			if !found {
				existingChild = &stack.Resource{
					Name:      name,
					Action:    content.Update.Action,
					StartTime: now,
				}

				parent.Children = append(parent.Children, existingChild)
			}

			if content.Update.Status == deploymentspb.ResourceDeploymentStatus_FAILED || content.Update.Status == deploymentspb.ResourceDeploymentStatus_SUCCESS || content.Update.Action == deploymentspb.ResourceDeploymentAction_SAME {
				existingChild.FinishTime = now
			}

			// update its status
			existingChild.Status = content.Update.Status
			existingChild.Message = content.Update.Message
		case *deploymentspb.DeploymentUpEvent_Result:
			m.resultOutput = content.Result.GetMarkdown()
		default:
			// discard for now
		}

		return m, reactive.AwaitChannel(msg.Source)
	case reactive.ChanMsg[error]:
		m.errs = append(m.errs, msg.Value)
		return m, nil
	case spinner.TickMsg:
		m.spinner, cmd = m.spinner.Update(msg)
	}
	return m, cmd
}

const maxOutputLines = 5

// Extend base glamor style
func nitricMdStyle() ansi.StyleConfig {
	baseStyle := glamour.DraculaStyleConfig

	baseStyle.Link.Color = &tui.Colors.Purple.TrueColor
	baseStyle.Heading.Color = &tui.Colors.Blue.TrueColor

	return baseStyle
}

var (
	terminalBorderStyle = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), true, false, true, false).BorderForeground(tui.Colors.Purple)
	errorStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
)

func (m Model) View() string {
	v := view.New()
	v.Break()
	v.Add(fragments.Tag("up"))
	v.Add("  Deploying with %s", m.provider)
	if m.done {
		v.Break()
	} else {
		v.Addln(m.spinner.View())
	}
	v.Break()

	statusTree := fragments.NewStatusNode("stack", "")

	for _, child := range m.stack.Children {
		currentNode := statusTree.AddNode(child.Name, "")

		for _, grandchild := range child.Children {
			resourceTime := lo.Ternary(grandchild.FinishTime.IsZero(), time.Since(grandchild.StartTime).Round(time.Second), grandchild.FinishTime.Sub(grandchild.StartTime))
			statusColor := tui.Colors.Blue
			if grandchild.Status == deploymentspb.ResourceDeploymentStatus_FAILED {
				statusColor = tui.Colors.Red
			} else if grandchild.Status == deploymentspb.ResourceDeploymentStatus_SUCCESS || grandchild.Action == deploymentspb.ResourceDeploymentAction_SAME {
				statusColor = tui.Colors.Green
			}

			statusText := fmt.Sprintf("%s (%s)", stack.VerbMap[grandchild.Action][grandchild.Status], resourceTime.Round(time.Second))
			currentNode.AddNode(grandchild.Name, lipgloss.NewStyle().Foreground(statusColor).Render(statusText))
		}
	}

	margin := 10
	if m.windowSize.Width < 60 {
		margin = 0
	}

	// when the final output is rendered the available output width is 5 characters narrower than the window size.
	lastRunFix := 5

	v.Addln(statusTree.Render(m.windowSize.Width - margin - lastRunFix)).WithStyle(lipgloss.NewStyle().MarginLeft(margin))

	// Provider Stdout and Stderr rendering
	if len(m.providerStdout) > 0 {
		v.Addln("%s stdout:", m.provider).WithStyle(lipgloss.NewStyle().Bold(true).Foreground(tui.Colors.Blue))

		providerTerm := view.New(view.WithStyle(terminalBorderStyle))

		for i, line := range m.providerStdout[max(0, len(m.providerStdout)-maxOutputLines):] {
			providerTerm.Add(line).WithStyle(lipgloss.NewStyle().Width(min(m.windowSize.Width, 100)))
			if i < len(m.providerStdout)-1 {
				providerTerm.Break()
			}
		}

		v.Addln(providerTerm.Render())
	}

	for _, e := range m.errs[max(0, len(m.errs)-maxOutputLines):] {
		v.Break()
		v.Add(fragments.ErrorTag())
		v.Addln("  %s", e.Error()).WithStyle(errorStyle)
	}

	if m.resultOutput != "" {
		v.Break()
		r, _ := glamour.NewTermRenderer(
			// detect background color and pick either the default dark or light theme
			// glamour.WithAutoStyle(),

			glamour.WithStyles(nitricMdStyle()),
			// glamour.WithStylesFromJSONBytes(nitricStyle),
			// wrap output at specific width (default is 80)
			glamour.WithWordWrap(m.windowSize.Width),
		)

		output, _ := r.Render(m.resultOutput)

		v.Addln(output)

		// v.Addln(fragments.Tag("result"))
		// v.Addln("\n%s", m.resultOutput)
	}

	return v.Render()
}

func New(providerName string, stackName string, updatesChan <-chan *deploymentspb.DeploymentUpEvent, providerStdoutChan <-chan string, errorChan <-chan error) Model {
	orphanParent := &stack.Resource{
		Name:     fmt.Sprintf("Stack::%s", stackName),
		Message:  "",
		Action:   deploymentspb.ResourceDeploymentAction_SAME,
		Status:   deploymentspb.ResourceDeploymentStatus_PENDING,
		Children: []*stack.Resource{},
	}

	return Model{
		provider:           providerName,
		spinner:            spinner.New(spinner.WithSpinner(spinner.Ellipsis)),
		updatesChan:        updatesChan,
		providerStdoutChan: providerStdoutChan,
		errorChan:          errorChan,
		defaultParent:      orphanParent,
		stack: &stack.Resource{
			Name:    "stack",
			Message: "",
			Children: []*stack.Resource{
				orphanParent,
			},
		},
	}
}
