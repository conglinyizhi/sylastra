package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAndValidate(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TEST_LLM_KEY", "secret")
	prepareFakeCommand(t, dir, "better-edit-tools")

	writeFile(t, filepath.Join(dir, "llms.toml"), `
[[profiles]]
name = "demo"
api_style = "openai_chat"
base_url = "https://api.openai.com/v1"
model = "gpt-4.1-mini"
api_key_env = "TEST_LLM_KEY"
timeout = 30
`)
	writeFile(t, filepath.Join(dir, "llm.index.toml"), `active = "demo"`)
	writeFile(t, filepath.Join(dir, "app.toml"), `
[mcp]
command = "better-edit-tools"
args = ["--lang", "zh"]

[mcp.fallback]
enabled = true
`)

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.ActiveProfile.Name != "demo" {
		t.Fatalf("active profile = %s", loaded.ActiveProfile.Name)
	}
	key, err := loaded.ActiveProfile.ResolvedAPIKey()
	if err != nil {
		t.Fatalf("ResolvedAPIKey() error = %v", err)
	}
	if key != "secret" {
		t.Fatalf("api key = %q", key)
	}
}

func TestInvalidProfile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "llms.toml"), `
[[profiles]]
name = "broken"
api_style = "invalid"
base_url = ""
model = ""
`)
	_, err := LoadProfiles(filepath.Join(dir, "llms.toml"))
	if err == nil || !strings.Contains(err.Error(), "unsupported api_style") {
		t.Fatalf("expected invalid api style error, got %v", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func prepareFakeCommand(t *testing.T, dir, name string) {
	t.Helper()
	pathDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(pathDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", pathDir, err)
	}
	cmdPath := filepath.Join(pathDir, name)
	if err := os.WriteFile(cmdPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake command: %v", err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", pathDir+string(os.PathListSeparator)+oldPath)
}
