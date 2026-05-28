package config

import (
	"fmt"
	"os"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

func DefaultAppConfig() AppConfig {
	return AppConfig{
		MCP: MCPConfig{
			Command: "better-edit-tools",
			Args:    []string{"--lang", "zh"},
			Fallback: MCPFallbackConfig{
				Enabled: true,
			},
		},
	}
}

func LoadAppLoose(path string) (AppConfig, error) {
	var cfg AppConfig
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return DefaultAppConfig(), nil
		}
		return AppConfig{}, err
	}
	if err := readTOML(path, &cfg); err != nil {
		return AppConfig{}, fmt.Errorf("load %s: %w", path, err)
	}
	if strings.TrimSpace(cfg.MCP.Command) == "" {
		cfg.MCP = DefaultAppConfig().MCP
	}
	if len(cfg.MCP.Args) == 0 {
		cfg.MCP.Args = DefaultAppConfig().MCP.Args
	}
	return cfg, nil
}

func WriteLLMFiles(paths Paths, profiles []LLMProfile, active string) error {
	payload := llmsFileWrite{
		Profiles: make([]llmProfileWrite, 0, len(profiles)),
	}
	for _, profile := range profiles {
		payload.Profiles = append(payload.Profiles, toLLMProfileWrite(profile))
	}
	body, err := toml.Marshal(payload)
	if err != nil {
		return err
	}
	if err := os.WriteFile(paths.LLMs, body, 0o644); err != nil {
		return err
	}
	indexBody, err := toml.Marshal(llmIndexWrite{Active: active})
	if err != nil {
		return err
	}
	return os.WriteFile(paths.LLMIndex, indexBody, 0o644)
}

func WriteAppFile(path string, cfg AppConfig) error {
	body, err := toml.Marshal(toAppConfigWrite(cfg))
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

type llmsFileWrite struct {
	Profiles []llmProfileWrite `toml:"profiles"`
}

type llmProfileWrite struct {
	Name        string            `toml:"name"`
	APIStyle    string            `toml:"api_style"`
	BaseURL     string            `toml:"base_url"`
	Model       string            `toml:"model"`
	APIKey      string            `toml:"api_key,omitempty"`
	APIKeyEnv   string            `toml:"api_key_env,omitempty"`
	Headers     map[string]string `toml:"headers,omitempty"`
	Timeout     int               `toml:"timeout,omitempty"`
	MaxTokens   int               `toml:"max_tokens,omitempty"`
	Temperature *float32          `toml:"temperature,omitempty"`
	DisplayName string            `toml:"display_name,omitempty"`
}

type llmIndexWrite struct {
	Active string `toml:"active"`
}

type appConfigWrite struct {
	MCP       mcpConfigWrite       `toml:"mcp"`
	UI        uiConfigWrite        `toml:"ui,omitempty"`
	Bootstrap bootstrapConfigWrite `toml:"bootstrap,omitempty"`
}

type mcpConfigWrite struct {
	Command  string                 `toml:"command"`
	Args     []string               `toml:"args"`
	Env      map[string]string      `toml:"env,omitempty"`
	Fallback mcpFallbackConfigWrite `toml:"fallback,omitempty"`
}

type mcpFallbackConfigWrite struct {
	Enabled bool `toml:"enabled,omitempty"`
}

type uiConfigWrite struct {
	Theme string `toml:"theme,omitempty"`
}

type bootstrapConfigWrite struct {
	LastMode     string `toml:"last_mode,omitempty"`
	LastSource   string `toml:"last_source,omitempty"`
	LastProfile  string `toml:"last_profile,omitempty"`
	ConfiguredAt string `toml:"configured_at,omitempty"`
	ReplacedAll  bool   `toml:"replaced_all,omitempty"`
}

func toLLMProfileWrite(profile LLMProfile) llmProfileWrite {
	return llmProfileWrite{
		Name:        profile.Name,
		APIStyle:    profile.APIStyle,
		BaseURL:     profile.BaseURL,
		Model:       profile.Model,
		APIKey:      profile.APIKey,
		APIKeyEnv:   profile.APIKeyEnv,
		Headers:     profile.Headers,
		Timeout:     profile.Timeout,
		MaxTokens:   profile.MaxTokens,
		Temperature: profile.Temperature,
		DisplayName: profile.DisplayName,
	}
}

func toAppConfigWrite(cfg AppConfig) appConfigWrite {
	return appConfigWrite{
		MCP: mcpConfigWrite{
			Command: cfg.MCP.Command,
			Args:    cfg.MCP.Args,
			Env:     cfg.MCP.Env,
			Fallback: mcpFallbackConfigWrite{
				Enabled: cfg.MCP.Fallback.Enabled,
			},
		},
		UI: uiConfigWrite{
			Theme: cfg.UI.Theme,
		},
		Bootstrap: bootstrapConfigWrite{
			LastMode:     cfg.Bootstrap.LastMode,
			LastSource:   cfg.Bootstrap.LastSource,
			LastProfile:  cfg.Bootstrap.LastProfile,
			ConfiguredAt: cfg.Bootstrap.ConfiguredAt,
			ReplacedAll:  cfg.Bootstrap.ReplacedAll,
		},
	}
}
