package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// ── Glamour Markdown renderer ──────────────────────────────────────

var mdRenderer *glamour.TermRenderer

func init() {
	mdRenderer, _ = glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(80),
	)
}

func renderMarkdown(text string) string {
	if mdRenderer == nil || strings.TrimSpace(text) == "" {
		return text
	}
	out, err := mdRenderer.Render(text)
	if err != nil {
		return text
	}
	return strings.TrimRight(out, "\n")
}

// ── Prefix rendering ───────────────────────────────────────────────

func (m *Model) renderPrefix(kind BlockKind) string {
	switch kind {
	case BlockUser:
		return m.styles.prefixUser.Render(m.prefixCfg.User.Text)
	case BlockAIThink:
		return m.styles.prefixAIThink.Render(m.prefixCfg.AIThink.Text)
	case BlockAIToolUse:
		return m.styles.prefixToolUse.Render(m.prefixCfg.AIToolUse.Text)
	case BlockPCReturn:
		return m.styles.prefixPCRet.Render(m.prefixCfg.PCToolReturn.Text)
	case BlockError:
		return m.styles.errorLabel.Render("Error:")
	default:
		return "???"
	}
}

// ── Status indicator ───────────────────────────────────────────────

const (
	statusRunningStr = " ..."
	statusSuccessStr = " \u2713" // ✓
	statusFailedStr  = " \u2717" // ✗
)

func (m *Model) statusIndicator(status ToolStatus) string {
	switch status {
	case ToolRunning:
		return m.styles.statusRunning.Render(statusRunningStr)
	case ToolSuccess:
		return m.styles.statusSuccess.Render(statusSuccessStr)
	case ToolFailed:
		return m.styles.statusFailed.Render(statusFailedStr)
	default:
		return ""
	}
}

// ── Block rendering ────────────────────────────────────────────────

// renderBlock converts a single Block into one or more rendered lines.
func (m *Model) renderBlock(b *Block) []string {
	// Use cached rendered output if available and not dirty
	if !b.RenderDirty && b.Rendered != "" && b.RenderWidth == m.width {
		return splitLines(b.Rendered)
	}

	prefix := m.renderPrefix(b.Kind)

	// Determine body content with Markdown rendering for AI think blocks
	body := b.Content
	if strings.TrimSpace(body) == "" && m.busy && b.Kind == BlockAIThink {
		body = m.styles.placeholder.Render("...")
	} else if b.Kind == BlockAIThink && strings.TrimSpace(body) != "" {
		body = renderMarkdown(body)
	}

	indicator := m.statusIndicator(b.Meta.ToolStatus)

	// Calculate available width for body text
	avail := m.width - lipgloss.Width(prefix) - 2
	if indicator != "" {
		avail -= lipgloss.Width(indicator)
	}
	if avail < 10 {
		avail = 10
	}

	// Truncate body if needed
	bodyStr := body
	if lipgloss.Width(bodyStr) > avail {
		bodyStr = lipgloss.NewStyle().MaxWidth(avail).Render(bodyStr)
	}

	line := prefix + " " + bodyStr
	if indicator != "" {
		line += " " + indicator
	}

	lines := []string{line}

	// Render children (e.g., tool_return nested under tool_use)
	for _, child := range b.Children {
		childLines := m.renderBlock(child)
		for _, cl := range childLines {
			lines = append(lines, m.styles.body.Render(cl))
		}
	}

	// Cache the rendered output
	b.Rendered = strings.Join(lines, "\n")
	b.RenderWidth = m.width
	b.RenderDirty = false

	return lines
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// ── Token bar ──────────────────────────────────────────────────────

func (m *Model) renderContextBar() string {
	if m.contextTotal <= 0 {
		return ""
	}
	pct := float64(m.contextUsed) / float64(m.contextTotal) * 100
	barWidth := 20
	filled := int(pct / 100 * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	return "Context: " + bar + " " + formatContext(m.contextUsed, m.contextTotal) + " (" + formatPct(pct) + ")"
}

func formatContext(used, total int) string {
	return itoa(used/1000) + "k/" + itoa(total/1000) + "k"
}

func formatPct(pct float64) string {
	return itoa(int(pct)) + "%"
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return intString(n)
}

func intString(n int) string {
	if n == 0 {
		return "0"
	}
	digits := make([]byte, 0, 10)
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	// Reverse
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}
