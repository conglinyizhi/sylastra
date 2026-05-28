package config

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/conglinyizhi/sylastra/internal/appmeta"
)

const (
	APIStyleOpenAIChat        = "openai_chat"
	APIStyleOpenAIResponses   = "openai_responses"
	APIStyleAnthropicMessages = "anthropic_messages"
)

var supportedAPIStyles = map[string]struct{}{
	APIStyleOpenAIChat:        {},
	APIStyleOpenAIResponses:   {},
	APIStyleAnthropicMessages: {},
}

type LLMProfile struct {
	Name        string            `mapstructure:"name"`
	APIStyle    string            `mapstructure:"api_style"`
	BaseURL     string            `mapstructure:"base_url"`
	Model       string            `mapstructure:"model"`
	APIKey      string            `mapstructure:"api_key"`
	APIKeyEnv   string            `mapstructure:"api_key_env"`
	Headers     map[string]string `mapstructure:"headers"`
	Timeout     int               `mapstructure:"timeout"`
	MaxTokens   int               `mapstructure:"max_tokens"`
	Temperature *float32          `mapstructure:"temperature"`
	DisplayName string            `mapstructure:"display_name"`
}

type LLMIndexConfig struct {
	Active string `mapstructure:"active"`
}

type MCPConfig struct {
	Command  string            `mapstructure:"command"`
	Args     []string          `mapstructure:"args"`
	Env      map[string]string `mapstructure:"env"`
	Fallback MCPFallbackConfig `mapstructure:"fallback"`
	Resolved MCPResolvedConfig `mapstructure:"-"`
}

type MCPFallbackConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type MCPResolvedConfig struct {
	Command string
	Source  string
}

type UIConfig struct {
	Theme string `mapstructure:"theme"`
}

type BootstrapConfig struct {
	LastMode     string `mapstructure:"last_mode"`
	LastSource   string `mapstructure:"last_source"`
	LastProfile  string `mapstructure:"last_profile"`
	ConfiguredAt string `mapstructure:"configured_at"`
	ReplacedAll  bool   `mapstructure:"replaced_all"`
}

type AppConfig struct {
	MCP       MCPConfig       `mapstructure:"mcp"`
	UI        UIConfig        `mapstructure:"ui"`
	Bootstrap BootstrapConfig `mapstructure:"bootstrap"`
}

type Paths struct {
	Dir      string
	LLMs     string
	LLMIndex string
	App      string
}

type Loaded struct {
	Paths         Paths
	Profiles      []LLMProfile
	Index         LLMIndexConfig
	ActiveProfile LLMProfile
	App           AppConfig
}

type llmsFile struct {
	Profiles []LLMProfile `mapstructure:"profiles"`
}

func ResolvePaths(dir string) (Paths, error) {
	if dir == "" {
		var err error
		dir, err = appmeta.DefaultConfigDir()
		if err != nil {
			return Paths{}, err
		}
	}

	return Paths{
		Dir:      dir,
		LLMs:     filepath.Join(dir, appmeta.LLMsConfigName),
		LLMIndex: filepath.Join(dir, appmeta.LLMIndexName),
		App:      filepath.Join(dir, appmeta.AppConfigName),
	}, nil
}

func Load(dir string) (*Loaded, error) {
	paths, err := ResolvePaths(dir)
	if err != nil {
		return nil, err
	}

	profiles, err := LoadProfiles(paths.LLMs)
	if err != nil {
		return nil, err
	}
	index, err := LoadIndex(paths.LLMIndex)
	if err != nil {
		return nil, err
	}
	appCfg, err := LoadApp(paths.App)
	if err != nil {
		return nil, err
	}
	appCfg.MCP.Resolved = ResolveMCPCommand(paths, appCfg.MCP)

	active, err := SelectActiveProfile(profiles, index.Active)
	if err != nil {
		return nil, err
	}
	if err := ValidateAppConfig(appCfg); err != nil {
		return nil, err
	}

	return &Loaded{
		Paths:         paths,
		Profiles:      profiles,
		Index:         index,
		ActiveProfile: active,
		App:           appCfg,
	}, nil
}

func LoadProfiles(path string) ([]LLMProfile, error) {
	var cfg llmsFile
	if err := readTOML(path, &cfg); err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}
	if len(cfg.Profiles) == 0 {
		return nil, fmt.Errorf("%s: no profiles configured", path)
	}
	if err := ValidateProfiles(cfg.Profiles); err != nil {
		return nil, err
	}

	return cfg.Profiles, nil
}

func LoadIndex(path string) (LLMIndexConfig, error) {
	var cfg LLMIndexConfig
	if err := readTOML(path, &cfg); err != nil {
		return LLMIndexConfig{}, fmt.Errorf("load %s: %w", path, err)
	}
	if strings.TrimSpace(cfg.Active) == "" {
		return LLMIndexConfig{}, fmt.Errorf("%s: active profile is required", path)
	}

	return cfg, nil
}

func LoadApp(path string) (AppConfig, error) {
	var cfg AppConfig
	if err := readTOML(path, &cfg); err != nil {
		return AppConfig{}, fmt.Errorf("load %s: %w", path, err)
	}

	return cfg, nil
}

func ValidateProfiles(profiles []LLMProfile) error {
	seen := make(map[string]struct{}, len(profiles))
	for _, profile := range profiles {
		if err := profile.Validate(); err != nil {
			return fmt.Errorf("profile %q: %w", profile.Name, err)
		}
		if _, ok := seen[profile.Name]; ok {
			return fmt.Errorf("duplicate profile name %q", profile.Name)
		}
		seen[profile.Name] = struct{}{}
	}

	return nil
}

func ValidateAppConfig(cfg AppConfig) error {
	if strings.TrimSpace(cfg.MCP.Resolved.Command) == "" {
		return errors.New("app.toml: no MCP command resolved; set mcp.command, install better-edit-tools into PATH, or place it under ~/.local/sylastra/mcp/bin/")
	}
	return nil
}

func ResolveMCPCommand(paths Paths, cfg MCPConfig) MCPResolvedConfig {
	if strings.TrimSpace(cfg.Command) != "" {
		if resolved, err := lookPathOrExact(cfg.Command); err == nil {
			return MCPResolvedConfig{Command: resolved, Source: "config"}
		}
	}

	if cfg.Fallback.Enabled {
		if local, err := DefaultFallbackMCPPath(); err == nil {
			if info, statErr := os.Stat(local); statErr == nil && !info.IsDir() {
				return MCPResolvedConfig{Command: local, Source: "fallback"}
			}
		}
	}

	if resolved, err := exec.LookPath("better-edit-tools"); err == nil {
		return MCPResolvedConfig{Command: resolved, Source: "path"}
	}

	return MCPResolvedConfig{}
}

func DefaultFallbackMCPPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "sylastra", "mcp", "bin", "better-edit-tools"), nil
}

func lookPathOrExact(command string) (string, error) {
	if filepath.IsAbs(command) {
		info, err := os.Stat(command)
		if err != nil {
			return "", err
		}
		if info.IsDir() {
			return "", fmt.Errorf("%s is a directory", command)
		}
		return command, nil
	}
	return exec.LookPath(command)
}

func SelectActiveProfile(profiles []LLMProfile, active string) (LLMProfile, error) {
	for _, profile := range profiles {
		if profile.Name == active {
			return profile, nil
		}
	}
	return LLMProfile{}, fmt.Errorf("active profile %q not found", active)
}

func SupportedAPIStyles() []string {
	values := make([]string, 0, len(supportedAPIStyles))
	for style := range supportedAPIStyles {
		values = append(values, style)
	}
	sort.Strings(values)
	return values
}

func (p LLMProfile) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("name is required")
	}
	if _, ok := supportedAPIStyles[p.APIStyle]; !ok {
		return fmt.Errorf("unsupported api_style %q", p.APIStyle)
	}
	if strings.TrimSpace(p.BaseURL) == "" {
		return errors.New("base_url is required")
	}
	if strings.TrimSpace(p.Model) == "" {
		return errors.New("model is required")
	}
	if strings.TrimSpace(p.APIKey) == "" && strings.TrimSpace(p.APIKeyEnv) == "" {
		return errors.New("api_key or api_key_env is required")
	}
	if p.Timeout < 0 {
		return errors.New("timeout must be >= 0")
	}
	if p.MaxTokens < 0 {
		return errors.New("max_tokens must be >= 0")
	}
	return nil
}

func (p LLMProfile) ResolvedAPIKey() (string, error) {
	if strings.TrimSpace(p.APIKey) != "" {
		return p.APIKey, nil
	}
	if strings.TrimSpace(p.APIKeyEnv) == "" {
		return "", errors.New("no api key configured")
	}
	value := strings.TrimSpace(os.Getenv(p.APIKeyEnv))
	if value == "" {
		return "", fmt.Errorf("environment variable %s is empty", p.APIKeyEnv)
	}
	return value, nil
}

func (p LLMProfile) HTTPTimeout() time.Duration {
	if p.Timeout <= 0 {
		return 2 * time.Minute
	}
	return time.Duration(p.Timeout) * time.Second
}

func readTOML(path string, out any) error {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("toml")
	if err := v.ReadInConfig(); err != nil {
		return err
	}
	return v.Unmarshal(out)
}
