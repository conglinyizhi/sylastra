package tui

import "github.com/charmbracelet/lipgloss"

type styles struct {
	// Container
	container lipgloss.Style

	// Block labels
	userLabel    lipgloss.Style
	userBox      lipgloss.Style
	aiLabel      lipgloss.Style
	toolsLabel   lipgloss.Style
	errorLabel   lipgloss.Style

	// Block body
	body lipgloss.Style
	meta lipgloss.Style

	// Input
	inputBox lipgloss.Style

	// Status
	status         lipgloss.Style
	placeholder    lipgloss.Style
	statusRunning  lipgloss.Style
	statusSuccess  lipgloss.Style
	statusFailed   lipgloss.Style

	// Status bar
	statusBar lipgloss.Style

	// Prefix colors
	prefixUser    lipgloss.Style
	prefixAIThink lipgloss.Style
	prefixToolUse lipgloss.Style
	prefixPCRet   lipgloss.Style
}

func defaultStyles() styles {
	return styles{
		container: lipgloss.NewStyle().Padding(0, 1),

		userLabel: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")),
		userBox: lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			Foreground(lipgloss.Color("255")).
			Padding(0, 1),
		aiLabel:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")),
		toolsLabel: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")),
		errorLabel: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("203")),

		body: lipgloss.NewStyle().PaddingLeft(2),
		meta: lipgloss.NewStyle().Foreground(lipgloss.Color("243")).PaddingLeft(2),

		inputBox: lipgloss.NewStyle().
			Background(lipgloss.Color("237")).
			Foreground(lipgloss.Color("255")).
			Padding(0, 1),

		status:        lipgloss.NewStyle().Foreground(lipgloss.Color("243")),
		placeholder:   lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		statusRunning: lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		statusSuccess: lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
		statusFailed:  lipgloss.NewStyle().Foreground(lipgloss.Color("203")),

		statusBar: lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")).
			PaddingLeft(1).
			PaddingRight(1),

		prefixUser:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")),
		prefixAIThink: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")),
		prefixToolUse: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")),
		prefixPCRet:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("112")),
	}
}

// ApplyPrefixColors updates prefix styles from config.
func (s *styles) ApplyPrefixColors(cfg PrefixConfig) {
	s.prefixUser = s.prefixUser.Foreground(lipgloss.Color(cfg.User.Color))
	s.prefixAIThink = s.prefixAIThink.Foreground(lipgloss.Color(cfg.AIThink.Color))
	s.prefixToolUse = s.prefixToolUse.Foreground(lipgloss.Color(cfg.AIToolUse.Color))
	s.prefixPCRet = s.prefixPCRet.Foreground(lipgloss.Color(cfg.PCToolReturn.Color))
}
