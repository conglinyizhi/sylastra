package tui

// DefaultPrefixConfig returns the default prefix configuration.
func DefaultPrefixConfig() PrefixConfig {
	return PrefixConfig{
		User: PrefixEntry{
			Text:  "User:",
			Color: "230",
		},
		AIThink: PrefixEntry{
			Text:  "AI: Think:",
			Color: "81",
		},
		AIToolUse: PrefixEntry{
			Text:  "AI: tool_use:",
			Color: "214",
		},
		PCToolReturn: PrefixEntry{
			Text:  "PC: tool_return:",
			Color: "112",
		},
	}
}

// MergePrefixConfig merges a partial config with defaults.
// Empty fields in cfg are filled from DefaultPrefixConfig().
func MergePrefixConfig(cfg PrefixConfig) PrefixConfig {
	def := DefaultPrefixConfig()

	if cfg.User.Text == "" {
		cfg.User.Text = def.User.Text
	}
	if cfg.User.Color == "" {
		cfg.User.Color = def.User.Color
	}
	if cfg.AIThink.Text == "" {
		cfg.AIThink.Text = def.AIThink.Text
	}
	if cfg.AIThink.Color == "" {
		cfg.AIThink.Color = def.AIThink.Color
	}
	if cfg.AIToolUse.Text == "" {
		cfg.AIToolUse.Text = def.AIToolUse.Text
	}
	if cfg.AIToolUse.Color == "" {
		cfg.AIToolUse.Color = def.AIToolUse.Color
	}
	if cfg.PCToolReturn.Text == "" {
		cfg.PCToolReturn.Text = def.PCToolReturn.Text
	}
	if cfg.PCToolReturn.Color == "" {
		cfg.PCToolReturn.Color = def.PCToolReturn.Color
	}

	return cfg
}
