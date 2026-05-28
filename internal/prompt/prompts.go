package prompt

import (
	"fmt"
	"strings"

	promptfs "github.com/conglinyizhi/sylastra"
)

func Load(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("prompt name is required")
	}

	body, err := promptfs.Files.ReadFile("prompts/zh/" + name + ".md")
	if err != nil {
		return "", fmt.Errorf("load prompt %q: %w", name, err)
	}

	return strings.TrimSpace(string(body)), nil
}
