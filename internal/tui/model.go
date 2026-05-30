package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/conglinyizhi/sylastra/internal/agent"
)

// ── Runtime interface ──────────────────────────────────────────────

// Runtime is the interface the TUI uses to run agent turns.
type Runtime interface {
	RunTurn(context.Context, *agent.Session, string, agent.Sink) error
}

// ── Messages ───────────────────────────────────────────────────────

type turnFinishedMsg struct {
	err error
}

type eventMsg struct {
	event agent.Event
}

// ── Sink ───────────────────────────────────────────────────────────

type sink struct {
	ch chan tea.Msg
}

func (s sink) Emit(event agent.Event) {
	s.ch <- eventMsg{event: event}
}

// ── Token tracker ──────────────────────────────────────────────────

type tokenTracker struct {
	totalTokens int
	startTime   time.Time
	speed       float64
}

func newTokenTracker() *tokenTracker {
	return &tokenTracker{startTime: time.Now()}
}

func (t *tokenTracker) addToken(count int) {
	t.totalTokens += count
	elapsed := time.Since(t.startTime).Seconds()
	if elapsed > 0 {
		t.speed = float64(t.totalTokens) / elapsed
	}
}

// ── Model ──────────────────────────────────────────────────────────

type Model struct {
	runtime Runtime
	session *agent.Session

	// Layout components
	history  viewport.Model
	input    textinput.Model

	// Data
	blocks []*Block
	events chan tea.Msg

	// Dimensions
	width  int
	height int

	// State
	busy     bool
	status   string
	liveInfo string
	aiIndex  int
	toolOpen bool

	// Config
	prefixCfg PrefixConfig
	styles    styles

	// Tool pairing
	lastToolPairID string
	toolSeq        int64

	// Token tracking
	tokenTracker *tokenTracker

	// Context tracking
	contextUsed  int
	contextTotal int

	// Active model name
	activeModel string
}

// ── Constructor ────────────────────────────────────────────────────

func New(runtime Runtime) Model {
	input := textinput.New()
	input.Placeholder = "Write or paste text here, then press Enter"
	input.Focus()
	input.CharLimit = 0
	input.Prompt = ""

	prefixCfg := DefaultPrefixConfig()
	s := defaultStyles()
	s.ApplyPrefixColors(prefixCfg)

	return Model{
		runtime:      runtime,
		session:      &agent.Session{},
		input:        input,
		history:      viewport.New(0, 0),
		events:       make(chan tea.Msg, 128),
		status:       "Ready",
		liveInfo:     "idle",
		aiIndex:      -1,
		prefixCfg:    prefixCfg,
		tokenTracker: newTokenTracker(),
		contextTotal: 128000, // default context window
	}
}

// ── Bubble Tea lifecycle ───────────────────────────────────────────

func (m Model) Init() tea.Cmd {
	return m.waitForEvent()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.history.Width = max(20, msg.Width-2)
		m.history.Height = max(8, msg.Height-6)
		m.fullRedraw()

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
			m.liveInfo = "request failed"
			m.addBlock(NewBlock(BlockError, "turn failed: "+msg.err.Error()))
		} else if m.status != "Error" {
			m.status = "Ready"
			m.liveInfo = "idle"
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

// ── View ───────────────────────────────────────────────────────────

func (m Model) View() string {
	historyView := m.styles.container.Render(m.history.View())
	statusZone := m.styles.meta.Render("AI-Status: " + m.liveInfo)
	statusBar := m.renderStatusBar()
	inputView := m.styles.inputBox.Width(max(20, m.width-2)).Render(m.input.View())

	return lipgloss.JoinVertical(
		lipgloss.Left,
		historyView,
		statusZone,
		statusBar,
		inputView,
	)
}

// ── Turn management ────────────────────────────────────────────────

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
	m.addBlock(NewBlock(BlockUser, text))
	m.addBlock(NewBlock(BlockAIThink, ""))
	m.aiIndex = len(m.blocks) - 1
	m.toolOpen = false
	m.busy = true
	m.status = "Streaming"
	m.liveInfo = "queued request"
	m.tokenTracker = newTokenTracker()
	m.refreshView()
}

// ── Event handling ─────────────────────────────────────────────────

func (m *Model) handleEvent(event agent.Event) {
	switch event.Type {
	case agent.EventStatus:
		m.liveInfo = strings.TrimSpace(event.Status)

	case agent.EventTextDelta:
		m.appendAssistantDelta(event.Text)
		m.tokenTracker.addToken(len(event.Text))

	case agent.EventToolStart:
		m.status = "Running tools"
		m.liveInfo = "running tool " + event.ToolName
		m.handleToolStart(event.ToolName, event.ToolInput)

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
		output := strings.TrimSpace(event.ToolOutput)
		if output != "" {
			line += " output=" + output
		}
		m.handleToolEnd(event.ToolName, output, event.Err)
		// Legacy path: also append as tool line for backward compat
		if m.toolOpen || len(m.blocks) == 0 || m.blocks[len(m.blocks)-1].Kind != BlockAIToolUse {
			m.legacyAppendToolLine(line)
		}

	case agent.EventError:
		m.status = "Error"
		m.liveInfo = "model request failed"
		m.addBlock(NewBlock(BlockError, event.Err.Error()))

	case agent.EventDone:
		m.status = "Done"
		m.liveInfo = "response complete"

	case agent.EventNetworkStep:
		m.liveInfo = "🌐 " + strings.TrimSpace(event.Text)

	case agent.EventTokenUsage:
		if event.TokenInput > 0 {
			m.contextUsed += event.TokenInput
		}
		if event.TokenOutput > 0 {
			m.contextUsed += event.TokenOutput
		}
		if event.RequestID != "" {
			m.status = "req: " + event.RequestID
		}
	}
}

// ── Tool start/end ─────────────────────────────────────────────────

func (m *Model) handleToolStart(name, input string) {
	m.toolSeq++
	pairID := fmt.Sprintf("tool_%d", m.toolSeq)
	m.lastToolPairID = pairID

	block := NewBlock(BlockAIToolUse, name)
	block.Meta = BlockMeta{
		ToolName:   name,
		ToolInput:  input,
		ToolStatus: ToolRunning,
	}
	block.PairID = pairID
	block.Pending = true

	m.addBlock(block)
	m.toolOpen = true
}

func (m *Model) handleToolEnd(name, output string, err error) {
	status := ToolSuccess
	content := truncateOutput(output)
	if err != nil {
		status = ToolFailed
		content = fmt.Sprintf("错误: %s", err.Error())
	}

	retBlock := NewBlock(BlockPCReturn, content)
	retBlock.Meta = BlockMeta{
		ToolName:   name,
		ToolOutput: output,
		ToolStatus: status,
	}
	retBlock.PairID = m.lastToolPairID
	retBlock.Pending = false

	// Nest as child of the last tool_use block if possible
	if len(m.blocks) >= 2 {
		last := m.blocks[len(m.blocks)-1]
		if last.Kind == BlockAIToolUse && last.PairID == m.lastToolPairID {
			last.Meta.ToolStatus = status
			last.Pending = false
			last.AddChild(retBlock)
			return
		}
	}

	// Fallback: append as sibling
	m.addBlock(retBlock)
}

func truncateOutput(output string) string {
	const maxOutput = 200
	if len(output) > maxOutput {
		return output[:maxOutput] + "..."
	}
	return output
}

// ── Block management ───────────────────────────────────────────────

func (m *Model) addBlock(b *Block) {
	m.blocks = append(m.blocks, b)
}

func (m *Model) fullRedraw() {
	for _, b := range m.blocks {
		b.RenderDirty = true
	}
	m.refreshView()
}

// ── View refresh ───────────────────────────────────────────────────

func (m *Model) refreshView() {
	lines := make([]string, 0, len(m.blocks)*3)
	for _, b := range m.blocks {
		rendered := m.renderBlock(b)
		lines = append(lines, rendered...)
		lines = append(lines, "")
	}
	m.history.SetContent(strings.TrimRight(strings.Join(lines, "\n"), "\n"))
	m.history.GotoBottom()
}

// ── Assistant delta ────────────────────────────────────────────────

func (m *Model) appendAssistantDelta(delta string) {
	if m.aiIndex < 0 || m.aiIndex >= len(m.blocks) {
		b := NewBlock(BlockAIThink, delta)
		m.addBlock(b)
		m.aiIndex = len(m.blocks) - 1
		return
	}
	m.blocks[m.aiIndex].Content += delta
	m.blocks[m.aiIndex].MarkDirty()
}

// ── Legacy tool line (backward compat) ─────────────────────────────

// Deprecated: kept for backward compatibility during the refactor.
// Will be removed when all tool rendering uses BlockAIToolUse+BlockPCReturn pairs.
func (m *Model) legacyAppendToolLine(line string) {
	if !m.toolOpen || len(m.blocks) == 0 || m.blocks[len(m.blocks)-1].Kind != BlockAIToolUse {
		b := NewBlock(BlockAIToolUse, line)
		m.addBlock(b)
		m.toolOpen = true
		return
	}
	last := m.blocks[len(m.blocks)-1]
	last.Content += "\n" + line
	last.MarkDirty()
}

// ── Status bar ─────────────────────────────────────────────────────

func (m *Model) renderStatusBar() string {
	var parts []string

	if m.tokenTracker != nil && m.tokenTracker.speed > 0 {
		parts = append(parts, fmt.Sprintf("Token: %.1f/s", m.tokenTracker.speed))
	}

	ctxBar := m.renderContextBar()
	if ctxBar != "" {
		parts = append(parts, ctxBar)
	}

	if m.activeModel != "" {
		parts = append(parts, "Model: "+m.activeModel)
	}

	bar := strings.Join(parts, "  │  ")
	if bar == "" {
		bar = "Ready"
	}

	return m.styles.statusBar.Render(bar)
}

// ── Helpers ────────────────────────────────────────────────────────

func formatInlineSuffix(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return ": " + value
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
