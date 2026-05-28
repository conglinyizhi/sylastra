package appmeta

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	AppName       = "sylastra"
	ConfigDirName = AppName
	AppTitle      = "Sylastra"

	LLMsConfigName  = "llms.toml"
	LLMIndexName    = "llm.index.toml"
	AppConfigName   = "app.toml"
	DefaultLanguage = "zh"
)

func DefaultConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}

	return filepath.Join(home, ".config", ConfigDirName), nil
}
