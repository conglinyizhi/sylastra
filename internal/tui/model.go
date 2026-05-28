package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/conglinyizhi/sylastra/internal/agent"
)

type Runtime interface {
	RunTurn(context.Context, *agent.Session, string, agent.Sink) error
}

type blockKind string

const (
	blockUser  blockKind = "user"
	blockAI    blockKind = "ai"
	blockTools blockKind = "tools"
	blockError blockKind = "error"
)

type block struct {
	kind blockKind
	text string
	meta string
}

type Model struct {
	runtime Runtime
	session *agent.Session

	width  int
	height int

	input    textinput.Model
	content  viewport.Model
	blocks   []block
	events   chan tea.Msg
	busy     bool
	status   string
	liveInfo string
	aiIndex  int
	toolOpen bool

	styles styles
}

type styles struct {
	container   lipgloss.Style
	userLabel   lipgloss.Style
	userBox     lipgloss.Style
	aiLabel     lipgloss.Style
	toolsLabel  lipgloss.Style
	errorLabel  lipgloss.Style
	body        lipgloss.Style
	meta        lipgloss.Style
	inputBox    lipgloss.Style
	status      lipgloss.Style
	placeholder lipgloss.Style
}

type turnFinishedMsg struct {
	err error
}

type eventMsg struct {
	event agent.Event
}

type sink struct {
	ch chan tea.Msg
}

func New(runtime Runtime) Model {
	input := textinput.New()
	input.Placeholder = "Write or paste text here, then press Enter"
	input.Focus()
	input.CharLimit = 0
	input.Prompt = ""

	return Model{
		runtime:  runtime,
		session:  &agent.Session{},
		input:    input,
		content:  viewport.New(0, 0),
		events:   make(chan tea.Msg, 128),
		status:   "Ready",
		liveInfo: "idle",
		aiIndex:  -1,
		styles: styles{
			container: lipgloss.NewStyle().Padding(0, 1),
			userLabel: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")),
			userBox: lipgloss.NewStyle().
				Background(lipgloss.Color("236")).
				Foreground(lipgloss.Color("255")).
				Padding(0, 1),
			aiLabel:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")),
			toolsLabel: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")),
			errorLabel: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203")),
			body:       lipgloss.NewStyle().PaddingLeft(2),
			meta:       lipgloss.NewStyle().Foreground(lipgloss.Color("243")).PaddingLeft(2),
			inputBox: lipgloss.NewStyle().
				Background(lipgloss.Color("237")).
				Foreground(lipgloss.Color("255")).
				Padding(0, 1),
			status:      lipgloss.NewStyle().Foreground(lipgloss.Color("243")),
			placeholder: lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		},
	}
}

func (m Model) Init() tea.Cmd {
	return m.waitForEvent()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.content.Width = max(20, msg.Width-2)
		m.content.Height = max(8, msg.Height-6)
		m.refreshView()
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEnter:
			if m.busy {
				return m, nil
			}
			text := strings.TrimSpace(m.input.Value())
			if text == "" {
				return m, nil
			}
			m.startTurn(text)
			m.input.SetValue("")
			return m, tea.Batch(m.waitForEvent(), m.runTurn(text))
		}
	case eventMsg:
		m.handleEvent(msg.event)
		m.refreshView()
		return m, m.waitForEvent()
	case turnFinishedMsg:
		m.busy = false
		if msg.err != nil {
			m.status = "Failed"
			m.appendToolLine("turn failed: " + msg.err.Error())
		} else if m.status != "Error" {
			m.status = "Ready"
		}
		m.aiIndex = -1
		m.toolOpen = false
		m.refreshView()
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	input := m.styles.inputBox.Width(max(20, m.width-2)).Render(m.input.View())
	info := m.styles.meta.Render("AI-Status: " + m.liveInfo)
	status := m.styles.status.Render("Status: " + m.status + "  |  Enter submit  |  Ctrl+C quit")
	return lipgloss.JoinVertical(lipgloss.Left, m.styles.container.Render(m.content.View()), info, input, status)
}

func (m Model) runTurn(text string) tea.Cmd {
	return func() tea.Msg {
		err := m.runtime.RunTurn(context.Background(), m.session, text, sink{ch: m.events})
		m.events <- turnFinishedMsg{err: err}
		return nil
	}
}

func (m Model) waitForEvent() tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-m.events
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *Model) startTurn(text string) {
	m.blocks = append(m.blocks, block{kind: blockUser, text: text})
	m.blocks = append(m.blocks, block{
		kind: blockAI,
		text: "",
		meta: "input tokens: -  output tokens: -  cache: -  request id: -",
	})
	m.aiIndex = len(m.blocks) - 1
	m.toolOpen = false
	m.busy = true
	m.status = "Streaming"
	m.liveInfo = "queued request"
	m.refreshView()
}

func (m *Model) handleEvent(event agent.Event) {
	switch event.Type {
	case agent.EventStatus:
		m.liveInfo = strings.TrimSpace(event.Status)
	case agent.EventTextDelta:
		m.appendAssistantDelta(event.Text)
	case agent.EventToolStart:
		m.status = "Running tools"
		m.liveInfo = "running tool " + event.ToolName
		m.appendToolLine("calling " + event.ToolName + formatInlineSuffix(event.ToolInput))
	case agent.EventToolEnd:
		line := "finished " + event.ToolName
		if event.Err != nil {
			line += " error=" + event.Err.Error()
			m.status = "Error"
			m.liveInfo = "tool failed: " + event.ToolName
		} else {
			m.status = "Streaming"
			m.liveInfo = "tool finished: " + event.ToolName
		}
		if output := strings.TrimSpace(event.ToolOutput); output != "" {
			line += " output=" + output
		}
		m.appendToolLine(line)
	case agent.EventError:
		m.status = "Error"
		m.liveInfo = "model request failed"
		m.blocks = append(m.blocks, block{kind: blockError, text: event.Err.Error()})
	case agent.EventDone:
		m.status = "Done"
		m.liveInfo = "response complete"
	}
}

func (m *Model) refreshView() {
	lines := make([]string, 0, len(m.blocks)*3)
	for _, item := range m.blocks {
		switch item.kind {
		case blockUser:
			lines = append(lines,
				m.styles.userLabel.Render("User:"),
				m.styles.userBox.Width(max(20, m.width-4)).Render(item.text),
			)
		case blockAI:
			body := item.text
			if strings.TrimSpace(body) == "" && m.busy {
				body = m.styles.placeholder.Render("...")
			}
			lines = append(lines,
				m.styles.aiLabel.Render("AI:"),
				m.styles.body.Render(body),
			)
			if strings.TrimSpace(item.meta) != "" {
				lines = append(lines, m.styles.meta.Render(item.meta))
			}
		case blockTools:
			lines = append(lines,
				m.styles.toolsLabel.Render("AI-Tools:"),
				m.styles.body.Render(item.text),
			)
		case blockError:
			lines = append(lines,
				m.styles.errorLabel.Render("AI-Error:"),
				m.styles.body.Render(item.text),
			)
		}
		lines = append(lines, "")
	}
	m.content.SetContent(strings.TrimRight(strings.Join(lines, "\n"), "\n"))
	m.content.GotoBottom()
}

func (m *Model) appendAssistantDelta(delta string) {
	if m.aiIndex < 0 || m.aiIndex >= len(m.blocks) {
		m.blocks = append(m.blocks, block{
			kind: blockAI,
			text: delta,
			meta: "input tokens: -  output tokens: -  cache: -  request id: -",
		})
		m.aiIndex = len(m.blocks) - 1
		return
	}
	m.blocks[m.aiIndex].text += delta
}

func (m *Model) appendToolLine(line string) {
	if !m.toolOpen || len(m.blocks) == 0 || m.blocks[len(m.blocks)-1].kind != blockTools {
		m.blocks = append(m.blocks, block{kind: blockTools, text: line})
		m.toolOpen = true
		return
	}
	m.blocks[len(m.blocks)-1].text += "\n" + line
}

func formatInlineSuffix(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return ": " + value
}

func (s sink) Emit(event agent.Event) {
	s.ch <- eventMsg{event: event}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
