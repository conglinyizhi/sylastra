package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/conglinyizhi/sylastra/internal/agent"
	"github.com/conglinyizhi/sylastra/internal/appmeta"
)

type Runtime interface {
	RunTurn(context.Context, *agent.Session, string, agent.Sink) error
}

type Model struct {
	runtime Runtime
	session *agent.Session

	width  int
	height int

	input      textinput.Model
	chat       viewport.Model
	logs       viewport.Model
	transcript []string
	logLines   []string

	busy   bool
	status string
	events chan tea.Msg

	styles styles
}

type styles struct {
	header lipgloss.Style
	panel  lipgloss.Style
	input  lipgloss.Style
	status lipgloss.Style
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
	input.Placeholder = "输入你的任务，回车提交"
	input.Focus()
	input.CharLimit = 0
	input.Prompt = "› "

	return Model{
		runtime: runtime,
		session: &agent.Session{},
		input:   input,
		chat:    viewport.New(0, 0),
		logs:    viewport.New(0, 0),
		events:  make(chan tea.Msg, 128),
		status:  "就绪",
		styles: styles{
			header: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")),
			panel:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1),
			input:  lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, 1),
			status: lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
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
		chatHeight := max(8, msg.Height-10)
		logHeight := max(4, msg.Height-chatHeight-6)
		m.chat.Width = msg.Width - 4
		m.chat.Height = chatHeight
		m.logs.Width = msg.Width - 4
		m.logs.Height = logHeight
		m.refreshViews()
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
			m.transcript = append(m.transcript, "你: "+text, "助手: ")
			m.refreshViews()
			m.input.SetValue("")
			m.busy = true
			m.status = "处理中"
			return m, tea.Batch(m.waitForEvent(), m.runTurn(text))
		}
	case eventMsg:
		switch msg.event.Type {
		case agent.EventTextDelta:
			m.appendAssistantDelta(msg.event.Text)
		case agent.EventToolStart:
			m.logLines = append(m.logLines, fmt.Sprintf("tool start: %s %s", msg.event.ToolName, msg.event.ToolInput))
		case agent.EventToolEnd:
			line := fmt.Sprintf("tool end: %s", msg.event.ToolName)
			if msg.event.Err != nil {
				line += " error=" + msg.event.Err.Error()
			}
			if strings.TrimSpace(msg.event.ToolOutput) != "" {
				line += " output=" + msg.event.ToolOutput
			}
			m.logLines = append(m.logLines, line)
		case agent.EventError:
			m.logLines = append(m.logLines, "error: "+msg.event.Err.Error())
			m.status = "错误"
		case agent.EventDone:
			m.status = "完成"
		}
		m.refreshViews()
		return m, m.waitForEvent()
	case turnFinishedMsg:
		m.busy = false
		if msg.err != nil {
			m.status = "失败"
			m.logLines = append(m.logLines, "turn failed: "+msg.err.Error())
		} else if m.status != "错误" {
			m.status = "就绪"
		}
		m.refreshViews()
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	header := m.styles.header.Render(appmeta.AppTitle)
	chat := m.styles.panel.Width(max(20, m.width-2)).Render(m.chat.View())
	logs := m.styles.panel.Width(max(20, m.width-2)).Render(m.logs.View())
	input := m.styles.input.Width(max(20, m.width-2)).Render(m.input.View())
	status := m.styles.status.Render("状态: " + m.status + "  |  Ctrl+C 退出")

	return lipgloss.JoinVertical(lipgloss.Left, header, chat, logs, input, status)
}

func (m Model) runTurn(text string) tea.Cmd {
	return func() tea.Msg {
		err := m.runtime.RunTurn(context.Background(), m.session, text, sink{ch: m.events})
		return turnFinishedMsg{err: err}
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

func (m *Model) refreshViews() {
	m.chat.SetContent(strings.Join(m.transcript, "\n"))
	start := 0
	if len(m.logLines) > 20 {
		start = len(m.logLines) - 20
	}
	m.logs.SetContent(strings.Join(m.logLines[start:], "\n"))
	m.chat.GotoBottom()
	m.logs.GotoBottom()
}

func (m *Model) appendAssistantDelta(delta string) {
	if len(m.transcript) == 0 {
		m.transcript = append(m.transcript, "助手: "+delta)
		return
	}
	m.transcript[len(m.transcript)-1] += delta
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
