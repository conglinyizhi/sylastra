package bootstrap

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/conglinyizhi/sylastra/internal/config"
)

func TestApplyFirstRun(t *testing.T) {
	oldClient := probeHTTPClient
	probeHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test-123456789012345678901234567890" {
			t.Fatalf("authorization header = %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	})}
	defer func() { probeHTTPClient = oldClient }()

	dir := t.TempDir()
	paths, err := config.ResolvePaths(dir)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ApplyFirstRun(context.Background(), paths, "sk-test-123456789012345678901234567890,gpt-4o,https://example.test/v1")
	if err != nil {
		t.Fatalf("ApplyFirstRun() error = %v", err)
	}
	if result.Profile.Name != "gpt-4o" {
		t.Fatalf("profile name = %q", result.Profile.Name)
	}
	loaded, err := config.LoadAppLoose(paths.App)
	if err != nil {
		t.Fatalf("LoadAppLoose() error = %v", err)
	}
	if loaded.Bootstrap.LastMode != string(ModeFirstRun) {
		t.Fatalf("last mode = %q", loaded.Bootstrap.LastMode)
	}
}

func TestApplyFastRunCodex(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	prepareFakeCommand(t, home, "better-edit-tools")
	writeFile(t, filepath.Join(home, ".codex", "auth.json"), `{"OPENAI_API_KEY":"sk-test-123456789012345678901234567890"}`)
	writeFile(t, filepath.Join(home, ".codex", "config.toml"), `
model = "gpt-4.1-mini"
`)

	dir := t.TempDir()
	paths, err := config.ResolvePaths(dir)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ApplyFastRun(paths, "codex")
	if err != nil {
		t.Fatalf("ApplyFastRun() error = %v", err)
	}
	if result.Profile.Model != "gpt-4.1-mini" {
		t.Fatalf("model = %q", result.Profile.Model)
	}
	loaded, err := config.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.ActiveProfile.Model != "gpt-4.1-mini" {
		t.Fatalf("loaded model = %q", loaded.ActiveProfile.Model)
	}
}

func prepareFakeCommand(t *testing.T, dir, name string) {
	t.Helper()
	pathDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(pathDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	cmdPath := filepath.Join(pathDir, name)
	if err := os.WriteFile(cmdPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", pathDir+string(os.PathListSeparator)+oldPath)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
