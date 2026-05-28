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
	if _, err := os.Stat(filepath.Join(dir, "llms.toml")); err != nil {
		t.Fatalf("llms.toml missing: %v", err)
	}
}
