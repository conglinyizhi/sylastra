package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteRoundTrip(t *testing.T) {
	dir := t.TempDir()
	prepareFakeCommand(t, dir, "better-edit-tools")

	paths, err := ResolvePaths(dir)
	if err != nil {
		t.Fatal(err)
	}
	temp := float32(0.2)
	profiles := []LLMProfile{{
		Name:        "demo",
		DisplayName: "Demo",
		APIStyle:    APIStyleOpenAIChat,
		BaseURL:     "https://api.openai.com/v1",
		Model:       "gpt-4.1-mini",
		APIKeyEnv:   "OPENAI_API_KEY",
		Timeout:     120,
		MaxTokens:   2048,
		Temperature: &temp,
		Headers: map[string]string{
			"X-Test": "yes",
		},
	}}
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteLLMFiles(paths, profiles, "demo"); err != nil {
		t.Fatalf("WriteLLMFiles() error = %v", err)
	}
	appCfg := DefaultAppConfig()
	appCfg.Bootstrap = BootstrapConfig{
		LastMode:     "first_run",
		LastProfile:  "demo",
		ConfiguredAt: "2026-05-28T00:00:00Z",
		ReplacedAll:  true,
	}
	if err := WriteAppFile(paths.App, appCfg); err != nil {
		t.Fatalf("WriteAppFile() error = %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.ActiveProfile.Name != "demo" {
		t.Fatalf("active profile = %q", loaded.ActiveProfile.Name)
	}
	if loaded.ActiveProfile.APIKeyEnv != "OPENAI_API_KEY" {
		t.Fatalf("api key env = %q", loaded.ActiveProfile.APIKeyEnv)
	}
	if loaded.App.Bootstrap.LastMode != "first_run" {
		t.Fatalf("bootstrap last mode = %q", loaded.App.Bootstrap.LastMode)
	}
	if loaded.App.Bootstrap.ReplacedAll != true {
		t.Fatalf("bootstrap replaced_all = %v", loaded.App.Bootstrap.ReplacedAll)
	}
	if loaded.App.MCP.Fallback.Enabled != true {
		t.Fatalf("fallback enabled = %v", loaded.App.MCP.Fallback.Enabled)
	}
	if _, err := os.Stat(filepath.Join(dir, "llms.toml")); err != nil {
		t.Fatalf("llms.toml missing: %v", err)
	}
}

func TestResolveMCPCommandFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	fallbackPath, err := DefaultFallbackMCPPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(fallbackPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fallbackPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	paths, err := ResolvePaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	resolved := ResolveMCPCommand(paths, MCPConfig{
		Fallback: MCPFallbackConfig{Enabled: true},
	})
	if resolved.Source != "fallback" {
		t.Fatalf("resolved source = %q", resolved.Source)
	}
	if resolved.Command != fallbackPath {
		t.Fatalf("resolved command = %q", resolved.Command)
	}
}

func TestResolveMCPCommandFromExternalAgentTOML(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	externalDir := filepath.Join(home, ".local", "share", "codex-tools", "bin")
	if err := os.MkdirAll(externalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	externalBin := filepath.Join(externalDir, "better-edit-tools")
	if err := os.WriteFile(externalBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "config.toml"), []byte("[mcp]\ncommand = \""+externalBin+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := ResolvePaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	resolved := ResolveMCPCommand(paths, MCPConfig{
		Fallback: MCPFallbackConfig{Enabled: true},
	})
	if resolved.Source != "agent:codex" {
		t.Fatalf("resolved source = %q", resolved.Source)
	}
	if resolved.Command != externalBin {
		t.Fatalf("resolved command = %q", resolved.Command)
	}
}

func TestResolveMCPCommandFromExternalAgentJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	externalDir := filepath.Join(home, ".local", "share", "opencode-tools", "bin")
	if err := os.MkdirAll(externalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	externalBin := filepath.Join(externalDir, "better-edit-tools")
	if err := os.WriteFile(externalBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"mcp":{"servers":[{"command":"` + externalBin + `"}]}}`
	if err := os.WriteFile(filepath.Join(configDir, "opencode.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	paths, err := ResolvePaths(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	resolved := ResolveMCPCommand(paths, MCPConfig{
		Fallback: MCPFallbackConfig{Enabled: true},
	})
	if resolved.Source != "agent:opencode" {
		t.Fatalf("resolved source = %q", resolved.Source)
	}
	if resolved.Command != externalBin {
		t.Fatalf("resolved command = %q", resolved.Command)
	}
}
