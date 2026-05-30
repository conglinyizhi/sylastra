package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/conglinyizhi/sylastra/internal/config"
)

var probeHTTPClient = &http.Client{Timeout: 20 * time.Second}

type Mode string

const (
	ModeFirstRun Mode = "first_run"
	ModeFastRun  Mode = "fast_run"
)

type Result struct {
	Mode       Mode
	Source     string
	Profile    config.LLMProfile
	Configured time.Time
}

func ApplyFirstRun(ctx context.Context, paths config.Paths, raw string) (*Result, error) {
	return ApplyFirstRunWithProgress(ctx, paths, raw, nil)
}

func ApplyFirstRunWithProgress(ctx context.Context, paths config.Paths, raw string, report func(string)) (*Result, error) {
	fields := splitAndClean(raw)
	if len(fields) < 2 {
		return nil, fmt.Errorf("usage: --first-run \"<api_key>,<model_name>[,<base_url>]\"")
	}
	reportProgress(report, "Parsing --first-run input...")
	detected := detectFields(fields)
	if detected.APIKey == "" {
		return nil, errors.New("could not identify an API key in --first-run input")
	}
	if detected.ModelName == "" {
		return nil, errors.New("could not identify a model name in --first-run input")
	}

	profile := buildProfileFromDetected(detected)
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	reportProgress(report, fmt.Sprintf("Prepared profile %q (%s).", profile.Name, profile.APIStyle))
	reportProgress(report, fmt.Sprintf("Probing API endpoint from %s ...", profile.BaseURL))
	if err := probeProfile(ctx, profile, report); err != nil {
		return nil, err
	}
	reportProgress(report, "Connection probe succeeded. Writing config files...")

	result := &Result{
		Mode:       ModeFirstRun,
		Profile:    profile,
		Configured: time.Now().UTC(),
	}
	if err := persist(paths, result); err != nil {
		return nil, err
	}
	reportProgress(report, fmt.Sprintf("Bootstrap finished. Active profile: %s", result.Profile.Name))
	return result, nil
}

func ApplyFastRun(paths config.Paths, source string) (*Result, error) {
	profile, err := importSource(strings.TrimSpace(strings.ToLower(source)))
	if err != nil {
		return nil, err
	}
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	result := &Result{
		Mode:       ModeFastRun,
		Source:     source,
		Profile:    profile,
		Configured: time.Now().UTC(),
	}
	if err := persist(paths, result); err != nil {
		return nil, err
	}
	return result, nil
}

type detectedInput struct {
	APIKey    string
	ModelName string
	BaseURL   string
}

func splitAndClean(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func detectFields(fields []string) detectedInput {
	var out detectedInput
	for _, field := range fields {
		lower := strings.ToLower(field)
		switch {
		case isURL(field):
			out.BaseURL = field
		case isAPIKey(field, lower):
			if out.APIKey == "" {
				out.APIKey = field
			}
		case isModelName(field, lower):
			if out.ModelName == "" {
				out.ModelName = field
			}
		default:
			if len(field) > 24 && out.APIKey == "" {
				out.APIKey = field
			} else if out.ModelName == "" {
				out.ModelName = field
			}
		}
	}
	return out
}

func buildProfileFromDetected(in detectedInput) config.LLMProfile {
	style := config.APIStyleOpenAIChat
	baseURL := in.BaseURL
	modelName := in.ModelName
	lowerModel := strings.ToLower(modelName)
	switch {
	case strings.HasPrefix(strings.ToLower(in.APIKey), "sk-ant-") || strings.Contains(lowerModel, "claude") || strings.Contains(strings.ToLower(baseURL), "anthropic"):
		style = config.APIStyleAnthropicMessages
		if baseURL == "" {
			baseURL = "https://api.anthropic.com/v1"
		}
	case strings.Contains(strings.ToLower(baseURL), "responses"):
		style = config.APIStyleOpenAIResponses
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
	default:
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
	}

	name := sanitizeName(modelName)
	if name == "" {
		name = "default"
	}
	apiKeyEnv := defaultAPIKeyEnv(style)
	return config.LLMProfile{
		Name:        name,
		DisplayName: modelName,
		APIStyle:    style,
		BaseURL:     baseURL,
		Model:       modelName,
		APIKey:      in.APIKey,
		APIKeyEnv:   apiKeyEnv,
		Timeout:     120,
		MaxTokens:   2048,
	}
}

func persist(paths config.Paths, result *Result) error {
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		return err
	}
	if err := exportAPIKey(result.Profile); err != nil {
		return err
	}

	// Merge new profile into existing profiles list (append, not replace)
	profiles := mergeProfiles(paths.LLMs, result.Profile)
	if err := config.WriteLLMFiles(paths, profiles, result.Profile.Name); err != nil {
		return err
	}

	appCfg, err := config.LoadAppLoose(paths.App)
	if err != nil {
		return err
	}
	appCfg.Bootstrap = config.BootstrapConfig{
		LastMode:     string(result.Mode),
		LastSource:   result.Source,
		LastProfile:  result.Profile.Name,
		ConfiguredAt: result.Configured.Format(time.RFC3339),
		ReplacedAll:  false,
	}
	return config.WriteAppFile(paths.App, appCfg)
}

func mergeProfiles(llmsPath string, newProfile config.LLMProfile) []config.LLMProfile {
	existing, err := config.LoadProfiles(llmsPath)
	if err != nil {
		// llms.toml doesn't exist or is empty — start fresh
		return []config.LLMProfile{newProfile}
	}

	// Check if a profile with the same name already exists
	for i, p := range existing {
		if p.Name == newProfile.Name {
			existing[i] = newProfile
			return existing
		}
	}

	// Append new profile
	return append(existing, newProfile)
}

func probeProfile(ctx context.Context, profile config.LLMProfile, report func(string)) error {
	apiKey, err := profile.ResolvedAPIKey()
	if err != nil {
		return err
	}
	candidates := probeCandidates(profile)
	var failures []string
	for idx, candidate := range candidates {
		reportProgress(report, fmt.Sprintf("Probe %d/%d: %s", idx+1, len(candidates), candidate.Endpoint))
		err := executeProbeCandidate(ctx, profile, apiKey, candidate)
		if err == nil {
			reportProgress(report, fmt.Sprintf("Probe matched: %s", candidate.Endpoint))
			return nil
		}
		failures = append(failures, fmt.Sprintf("%s %s: %v", candidate.Label, candidate.Endpoint, err))
	}
	return fmt.Errorf("connection test failed across %d probe candidates:\n- %s", len(failures), strings.Join(failures, "\n- "))
}

func reportProgress(report func(string), message string) {
	if report != nil && strings.TrimSpace(message) != "" {
		report(message)
	}
}

type probeCandidate struct {
	Label    string
	Endpoint string
	Body     map[string]any
}

func probeCandidates(profile config.LLMProfile) []probeCandidate {
	if profile.APIStyle == config.APIStyleAnthropicMessages {
		return buildAnthropicProbeCandidates(profile)
	}
	return buildOpenAICompatibleProbeCandidates(profile)
}

func buildAnthropicProbeCandidates(profile config.LLMProfile) []probeCandidate {
	baseVariants := baseURLVariants(profile.BaseURL)
	body := map[string]any{
		"model":      profile.Model,
		"max_tokens": 16,
		"messages": []map[string]any{
			{"role": "user", "content": []map[string]any{{"type": "text", "text": "Reply with exactly ok"}}},
		},
	}
	out := make([]probeCandidate, 0, len(baseVariants))
	for _, endpoint := range baseVariants {
		out = append(out, probeCandidate{
			Label:    "anthropic-messages",
			Endpoint: endpoint + "/messages",
			Body:     body,
		})
	}
	return out
}

func buildOpenAICompatibleProbeCandidates(profile config.LLMProfile) []probeCandidate {
	baseVariants := baseURLVariants(profile.BaseURL)
	chatBody := map[string]any{
		"model": profile.Model,
		"messages": []map[string]any{
			{"role": "user", "content": "Reply with exactly ok"},
		},
		"max_tokens": 16,
	}
	chatBodyNoMax := map[string]any{
		"model": profile.Model,
		"messages": []map[string]any{
			{"role": "user", "content": "Reply with exactly ok"},
		},
	}
	responsesBody := map[string]any{
		"model": profile.Model,
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "Reply with exactly ok"},
				},
			},
		},
		"max_output_tokens": 16,
	}
	var out []probeCandidate
	for _, base := range baseVariants {
		out = append(out,
			probeCandidate{Label: "openai-chat", Endpoint: base + "/chat/completions", Body: chatBody},
			probeCandidate{Label: "openai-chat-no-max", Endpoint: base + "/chat/completions", Body: chatBodyNoMax},
			probeCandidate{Label: "openai-responses", Endpoint: base + "/responses", Body: responsesBody},
		)
	}
	if profile.APIStyle == config.APIStyleOpenAIResponses {
		rotateOpenAIResponsesFirst(out)
	}
	return out
}

func rotateOpenAIResponsesFirst(candidates []probeCandidate) {
	if len(candidates) == 0 {
		return
	}
	var reordered []probeCandidate
	for _, candidate := range candidates {
		if candidate.Label == "openai-responses" {
			reordered = append(reordered, candidate)
		}
	}
	for _, candidate := range candidates {
		if candidate.Label != "openai-responses" {
			reordered = append(reordered, candidate)
		}
	}
	copy(candidates, reordered)
}

func executeProbeCandidate(ctx context.Context, profile config.LLMProfile, apiKey string, candidate probeCandidate) error {
	body, err := json.Marshal(candidate.Body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, candidate.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	switch profile.APIStyle {
	case config.APIStyleAnthropicMessages:
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	default:
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := probeHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return formatProbeHTTPError(resp)
	}
	return nil
}

func formatProbeHTTPError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if trimmed := strings.TrimSpace(string(body)); trimmed != "" {
		return fmt.Errorf("%s: %s", resp.Status, trimmed)
	}
	return fmt.Errorf("%s", resp.Status)
}

func baseURLVariants(raw string) []string {
	normalized := normalizeBaseURL(raw)
	seen := map[string]struct{}{}
	var out []string
	appendVariant := func(value string) {
		value = strings.TrimRight(value, "/")
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	appendVariant(normalized)
	if u, err := url.Parse(normalized); err == nil {
		path := strings.TrimRight(u.Path, "/")
		switch {
		case strings.HasSuffix(path, "/chat/completions"):
			u.Path = strings.TrimSuffix(path, "/chat/completions")
			appendVariant(u.String())
		case strings.HasSuffix(path, "/responses"):
			u.Path = strings.TrimSuffix(path, "/responses")
			appendVariant(u.String())
		case strings.HasSuffix(path, "/messages"):
			u.Path = strings.TrimSuffix(path, "/messages")
			appendVariant(u.String())
		}
		path = strings.TrimRight(u.Path, "/")
		if path == "" {
			u.Path = "/v1"
			appendVariant(u.String())
		} else if !strings.HasSuffix(path, "/v1") {
			u.Path = path + "/v1"
			appendVariant(u.String())
		}
		if strings.HasSuffix(path, "/v1") {
			u.Path = strings.TrimSuffix(path, "/v1")
			appendVariant(u.String())
		}
	}
	return out
}

func normalizeBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimRight(raw, "/")
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	return "https://" + raw
}

func sanitizeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "-")
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-_.")
}

func isURL(s string) bool {
	s = strings.ToLower(s)
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func isAPIKey(s, lower string) bool {
	prefixes := []string{"sk-", "sk-ant-", "gsk_", "pplx-", "xai-", "nvapi-", "wb_", "pat-", "ghp_"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return len(s) >= 32 && !strings.ContainsAny(s, " \t\n")
}

func isModelName(s, lower string) bool {
	modelSignals := []string{
		"gpt", "claude", "gemini", "deepseek", "qwen", "glm",
		"llama", "mistral", "mixtral", "phi", "command", "moonshot",
	}
	for _, signal := range modelSignals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return strings.ContainsAny(s, ".-")
}

type importResult struct {
	APIKey   string
	BaseURL  string
	Model    string
	APIStyle string
}

func importSource(source string) (config.LLMProfile, error) {
	var result importResult
	var err error
	switch source {
	case "codex":
		result, err = importCodex()
	case "claude":
		result, err = importClaude()
	case "opencode":
		result, err = importOpenCode()
	case "kimi":
		result, err = importKimi()
	default:
		return config.LLMProfile{}, fmt.Errorf("unknown fast-run source %q", source)
	}
	if err != nil {
		return config.LLMProfile{}, err
	}
	return config.LLMProfile{
		Name:        sanitizeName(result.Model),
		DisplayName: result.Model,
		APIStyle:    result.APIStyle,
		BaseURL:     result.BaseURL,
		Model:       result.Model,
		APIKeyEnv:   defaultAPIKeyEnv(result.APIStyle),
		Timeout:     120,
		MaxTokens:   2048,
	}, nil
}

func defaultAPIKeyEnv(apiStyle string) string {
	switch apiStyle {
	case config.APIStyleAnthropicMessages:
		return "ANTHROPIC_API_KEY"
	default:
		return "OPENAI_API_KEY"
	}
}

func exportAPIKey(profile config.LLMProfile) error {
	if strings.TrimSpace(profile.APIKey) == "" {
		return nil
	}
	if strings.TrimSpace(profile.APIKeyEnv) == "" {
		return errors.New("missing api_key_env for imported profile")
	}
	return os.Setenv(profile.APIKeyEnv, profile.APIKey)
}

func importCodex() (importResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return importResult{}, err
	}
	authPath := filepath.Join(home, ".codex", "auth.json")
	authData, err := os.ReadFile(authPath)
	if err != nil {
		return importResult{}, fmt.Errorf("codex auth not found: %w", err)
	}
	var auth struct {
		OpenAIAPIKey string `json:"OPENAI_API_KEY"`
	}
	if err := json.Unmarshal(authData, &auth); err != nil {
		return importResult{}, err
	}
	if strings.TrimSpace(auth.OpenAIAPIKey) == "" {
		return importResult{}, errors.New("no usable api key found in codex auth")
	}

	model := "gpt-4o"
	baseURL := "https://api.openai.com/v1"
	var cfg struct {
		Model          string `mapstructure:"model"`
		ModelProvider  string `mapstructure:"model_provider"`
		ModelProviders map[string]struct {
			BaseURL string `mapstructure:"base_url"`
			WireAPI string `mapstructure:"wire_api"`
		} `mapstructure:"model_providers"`
	}
	configPath := filepath.Join(home, ".codex", "config.toml")
	if _, err := os.Stat(configPath); err == nil {
		v := viper.New()
		v.SetConfigFile(configPath)
		v.SetConfigType("toml")
		if err := v.ReadInConfig(); err == nil && v.Unmarshal(&cfg) == nil {
			if cfg.Model != "" {
				model = cfg.Model
			}
			if cfg.ModelProvider != "" {
				if providerCfg, ok := cfg.ModelProviders[cfg.ModelProvider]; ok && providerCfg.BaseURL != "" {
					baseURL = providerCfg.BaseURL
				}
			}
		}
	}
	return importResult{
		APIKey:   auth.OpenAIAPIKey,
		BaseURL:  baseURL,
		Model:    model,
		APIStyle: config.APIStyleOpenAIChat,
	}, nil
}

func importClaude() (importResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return importResult{}, err
	}
	configPath := filepath.Join(home, ".claude", "claude.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return importResult{}, fmt.Errorf("claude config not found: %w", err)
	}
	var cfg struct {
		Projects map[string]struct {
			APIKey string `json:"api_key"`
			Model  string `json:"model"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return importResult{}, err
	}
	for _, project := range cfg.Projects {
		if project.APIKey != "" {
			model := project.Model
			if model == "" {
				model = "claude-sonnet-4-20250514"
			}
			return importResult{
				APIKey:   project.APIKey,
				BaseURL:  "https://api.anthropic.com/v1",
				Model:    model,
				APIStyle: config.APIStyleAnthropicMessages,
			}, nil
		}
	}
	return importResult{}, errors.New("no usable api key found in claude config")
}

func importOpenCode() (importResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return importResult{}, err
	}
	configPath := filepath.Join(home, ".config", "opencode", "opencode.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return importResult{}, fmt.Errorf("opencode config not found: %w", err)
	}

	// Parse with the modern flexible provider map structure
	var raw struct {
		Provider map[string]struct {
			Name    string `json:"name"`
			NPM     string `json:"npm"`
			Options struct {
				BaseURL string `json:"baseURL"`
				APIKey  string `json:"apiKey"`
			} `json:"options"`
			Models  map[string]struct {
				Name string `json:"name"`
			} `json:"models"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return importResult{}, err
	}

	// Iterate all providers, pick the first one with an apiKey
	for providerName, prov := range raw.Provider {
		if prov.Options.APIKey == "" {
			continue
		}

		apiKey := prov.Options.APIKey
		baseURL := prov.Options.BaseURL
		modelName := detectOpenCodeModel(prov.Models, providerName)
		apiStyle := detectOpenCodeStyle(providerName, prov.NPM, baseURL)

		if baseURL == "" {
			baseURL = defaultBaseURLForStyle(apiStyle)
		}
		if modelName == "" {
			modelName = defaultModelForStyle(apiStyle)
		}

		return importResult{
			APIKey:   apiKey,
			BaseURL:  baseURL,
			Model:    modelName,
			APIStyle: apiStyle,
		}, nil
	}

	// Fallback: try the old rigid provider-name lookup
	return importOpenCodeLegacy(data)
}

func detectOpenCodeModel(models map[string]struct {
	Name string `json:"name"`
}, _ string) string {
	// Prefer the Name field of any model entry
	for _, m := range models {
		if m.Name != "" {
			return m.Name
		}
	}
	// Fall back to the first key (model ID)
	for modelID := range models {
		return modelID
	}
	return ""
}

func detectOpenCodeStyle(providerName, npm, baseURL string) string {
	for _, s := range []string{providerName, npm, baseURL} {
		lower := strings.ToLower(s)
		if strings.Contains(lower, "anthropic") {
			return config.APIStyleAnthropicMessages
		}
	}
	return config.APIStyleOpenAIChat
}

func defaultBaseURLForStyle(apiStyle string) string {
	switch apiStyle {
	case config.APIStyleAnthropicMessages:
		return "https://api.anthropic.com/v1"
	default:
		return "https://api.openai.com/v1"
	}
}

func defaultModelForStyle(apiStyle string) string {
	switch apiStyle {
	case config.APIStyleAnthropicMessages:
		return "claude-sonnet-4-20250514"
	default:
		return "gpt-4o"
	}
}

// importOpenCodeLegacy handles the old rigid provider structure.
func importOpenCodeLegacy(data []byte) (importResult, error) {
	var cfg struct {
		Provider struct {
			OpenAI *struct {
				Options struct {
					BaseURL string `json:"baseURL"`
					APIKey  string `json:"apiKey"`
				} `json:"options"`
			} `json:"openai"`
			Anthropic *struct {
				Options struct {
					BaseURL string `json:"baseURL"`
					APIKey  string `json:"apiKey"`
				} `json:"options"`
			} `json:"anthropic"`
			DeepSeek *struct {
				Options struct {
					BaseURL string `json:"baseURL"`
					APIKey  string `json:"apiKey"`
				} `json:"options"`
			} `json:"deepseek"`
			Custom *struct {
				Options struct {
					BaseURL string `json:"baseURL"`
					APIKey  string `json:"apiKey"`
				} `json:"options"`
			} `json:"custom"`
		} `json:"provider"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return importResult{}, err
	}
	if cfg.Provider.OpenAI != nil && cfg.Provider.OpenAI.Options.APIKey != "" {
		baseURL := cfg.Provider.OpenAI.Options.BaseURL
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		return importResult{APIKey: cfg.Provider.OpenAI.Options.APIKey, BaseURL: baseURL, Model: "gpt-4o", APIStyle: config.APIStyleOpenAIChat}, nil
	}
	if cfg.Provider.Anthropic != nil && cfg.Provider.Anthropic.Options.APIKey != "" {
		baseURL := cfg.Provider.Anthropic.Options.BaseURL
		if baseURL == "" {
			baseURL = "https://api.anthropic.com/v1"
		}
		return importResult{APIKey: cfg.Provider.Anthropic.Options.APIKey, BaseURL: baseURL, Model: "claude-sonnet-4-20250514", APIStyle: config.APIStyleAnthropicMessages}, nil
	}
	if cfg.Provider.DeepSeek != nil && cfg.Provider.DeepSeek.Options.APIKey != "" {
		baseURL := cfg.Provider.DeepSeek.Options.BaseURL
		if baseURL == "" {
			baseURL = "https://api.deepseek.com/v1"
		}
		return importResult{APIKey: cfg.Provider.DeepSeek.Options.APIKey, BaseURL: baseURL, Model: "deepseek-chat", APIStyle: config.APIStyleOpenAIChat}, nil
	}
	if cfg.Provider.Custom != nil && cfg.Provider.Custom.Options.APIKey != "" {
		baseURL := cfg.Provider.Custom.Options.BaseURL
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		return importResult{APIKey: cfg.Provider.Custom.Options.APIKey, BaseURL: baseURL, Model: "custom-model", APIStyle: config.APIStyleOpenAIChat}, nil
	}
	return importResult{}, errors.New("no usable api key found in opencode config")
}

func importKimi() (importResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return importResult{}, err
	}
	var cfg struct {
		DefaultModel string `mapstructure:"default_model"`
		Providers    map[string]struct {
			Type    string `mapstructure:"type"`
			BaseURL string `mapstructure:"base_url"`
			APIKey  string `mapstructure:"api_key"`
		} `mapstructure:"providers"`
		Models map[string]struct {
			Provider string `mapstructure:"provider"`
			Model    string `mapstructure:"model"`
		} `mapstructure:"models"`
	}
	configPath := filepath.Join(home, ".kimi", "config.toml")
	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("toml")
	if err := v.ReadInConfig(); err != nil {
		return importResult{}, fmt.Errorf("kimi config not found: %w", err)
	}
	if err := v.Unmarshal(&cfg); err != nil {
		return importResult{}, err
	}
	for providerName, provider := range cfg.Providers {
		if provider.APIKey == "" {
			continue
		}
		model := cfg.DefaultModel
		for _, entry := range cfg.Models {
			if entry.Provider == providerName && entry.Model != "" {
				model = entry.Model
				break
			}
		}
		if model == "" {
			model = "moonshot-v1-8k"
		}
		baseURL := provider.BaseURL
		if baseURL == "" {
			baseURL = "https://api.moonshot.ai/v1"
		}
		return importResult{
			APIKey:   provider.APIKey,
			BaseURL:  baseURL,
			Model:    model,
			APIStyle: config.APIStyleOpenAIChat,
		}, nil
	}
	return importResult{}, errors.New("no usable api key found in kimi config")
}
