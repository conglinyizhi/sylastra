package prompt

import "testing"

func TestLoad(t *testing.T) {
	body, err := Load("system")
	if err != nil {
		t.Fatalf("Load(system) error = %v", err)
	}
	if body == "" {
		t.Fatalf("expected non-empty prompt")
	}
}

func TestMissingPrompt(t *testing.T) {
	if _, err := Load("missing"); err == nil {
		t.Fatalf("expected missing prompt error")
	}
}
