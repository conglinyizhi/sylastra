package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/conglinyizhi/sylastra/internal/agent"
	"github.com/conglinyizhi/sylastra/internal/appmeta"
	"github.com/conglinyizhi/sylastra/internal/bootstrap"
	"github.com/conglinyizhi/sylastra/internal/config"
	"github.com/conglinyizhi/sylastra/internal/llm"
	"github.com/conglinyizhi/sylastra/internal/prompt"
	"github.com/conglinyizhi/sylastra/internal/tools"
	"github.com/conglinyizhi/sylastra/internal/tui"
)

func main() {
	cmd := newRootCmd()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   appmeta.AppName,
		Short: appmeta.AppTitle + " — a small TUI coding agent",
		Long: `Sylastra is a small Go TUI coding agent.

It runs with:
  - a local config directory (~/.config/sylastra/)
  - one active LLM profile
  - one MCP server over stdio`,
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	root.AddCommand(newConfigCmd())
	root.AddCommand(newTUICmd())

	return root
}

// ── config ──────────────────────────────────────────────────────────

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage Sylastra configuration",
	}

	cmd.AddCommand(newConfigInitCmd())
	cmd.AddCommand(newConfigValidateCmd())
	cmd.AddCommand(newConfigShowActiveCmd())

	return cmd
}

func newConfigInitCmd() *cobra.Command {
	var firstRun string
	var fastRun string
	var force bool

	c := &cobra.Command{
		Use:   "init",
		Short: "Create example or bootstrapped config files",
		RunE: func(_ *cobra.Command, _ []string) error {
			paths, err := config.ResolvePaths("")
			if err != nil {
				return err
			}

			ctx := context.Background()

			switch {
			case firstRun != "":
				result, err := bootstrap.ApplyFirstRunWithProgress(ctx, paths, firstRun, func(msg string) {
					fmt.Fprintln(os.Stderr, msg)
				})
				if err != nil {
					return fmt.Errorf("first-run bootstrap: %w", err)
				}
				fmt.Fprintf(os.Stderr, "Active profile: %s (%s/%s)\n",
					result.Profile.Name, result.Profile.Model, result.Profile.BaseURL)

			case fastRun != "":
				result, err := bootstrap.ApplyFastRun(paths, fastRun)
				if err != nil {
					return fmt.Errorf("fast-run bootstrap (%s): %w", fastRun, err)
				}
				fmt.Fprintf(os.Stderr, "Imported %q -> active profile: %s (%s)\n",
					fastRun, result.Profile.Name, result.Profile.Model)

			default:
				if err := writeExampleConfig(paths, force); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "Example config files created in %s\n", paths.Dir)
				fmt.Fprintf(os.Stderr, "Edit llms.toml to add your LLM profile, then run 'sylastra tui run'\n")
			}

			return nil
		},
	}

	c.Flags().StringVar(&firstRun, "first-run", "", `Bootstrap from compact input: "sk-xxx,gpt-4o[,base_url]"`)
	c.Flags().StringVar(&fastRun, "fast-run", "", "Import profiles from another agent (codex|claude|opencode|kimi)")
	c.Flags().BoolVar(&force, "force", false, "Overwrite existing config files")

	return c
}

func writeExampleConfig(paths config.Paths, force bool) error {
	// Check if files already exist
	for _, p := range []string{paths.LLMs, paths.LLMIndex, paths.App} {
		if _, err := os.Stat(p); err == nil && !force {
			return fmt.Errorf("%s already exists, use --force to overwrite", p)
		}
	}

	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		return err
	}

	// Write default app.toml
	appCfg := config.DefaultAppConfig()
	if err := config.WriteAppFile(paths.App, appCfg); err != nil {
		return err
	}

	// Write example llms.toml + llm.index.toml with a placeholder profile
	exampleProfile := config.LLMProfile{
		Name:        "example-openai",
		DisplayName: "Example OpenAI",
		APIStyle:    config.APIStyleOpenAIChat,
		BaseURL:     "https://api.openai.com/v1",
		Model:       "gpt-4.1-mini",
		APIKeyEnv:   "OPENAI_API_KEY",
		Timeout:     120,
		MaxTokens:   2048,
	}
	if err := config.WriteLLMFiles(paths, []config.LLMProfile{exampleProfile}, "example-openai"); err != nil {
		return err
	}

	return nil
}

func newConfigValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the current configuration",
		RunE: func(_ *cobra.Command, _ []string) error {
			loaded, err := config.Load("")
			if err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "Config directory: %s\n", loaded.Paths.Dir)
			fmt.Fprintf(os.Stderr, "Active profile : %s (%s/%s)\n",
				loaded.ActiveProfile.Name, loaded.ActiveProfile.Model, loaded.ActiveProfile.APIStyle)
			fmt.Fprintf(os.Stderr, "MCP command    : %s (source: %s)\n",
				loaded.App.MCP.Resolved.Command, loaded.App.MCP.Resolved.Source)
			fmt.Fprintln(os.Stderr, "Configuration is valid.")
			return nil
		},
	}
}

func newConfigShowActiveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show-active",
		Short: "Show the active LLM profile",
		RunE: func(_ *cobra.Command, _ []string) error {
			paths, err := config.ResolvePaths("")
			if err != nil {
				return err
			}
			profiles, err := config.LoadProfiles(paths.LLMs)
			if err != nil {
				return fmt.Errorf("load profiles: %w", err)
			}
			index, err := config.LoadIndex(paths.LLMIndex)
			if err != nil {
				return fmt.Errorf("load index: %w", err)
			}
			active, err := config.SelectActiveProfile(profiles, index.Active)
			if err != nil {
				return err
			}

			fmt.Printf("Active profile: %s\n", active.Name)
			fmt.Printf("  API style : %s\n", active.APIStyle)
			fmt.Printf("  Base URL  : %s\n", active.BaseURL)
			fmt.Printf("  Model     : %s\n", active.Model)
			fmt.Printf("  API key   : %s\n", maskKey(active.APIKey, active.APIKeyEnv))

			return nil
		},
	}
}

func maskKey(key, env string) string {
	if env != "" {
		return "$" + env
	}
	if len(key) > 8 {
		return key[:4] + "****" + key[len(key)-4:]
	}
	return "****"
}

// ── tui ─────────────────────────────────────────────────────────────

func newTUICmd() *cobra.Command {
	var fastRun string

	c := &cobra.Command{
		Use:   "tui",
		Short: "Start the TUI chat interface",
	}

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Start the terminal user interface",
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Handle graceful shutdown
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				cancel()
			}()

			paths, err := config.ResolvePaths("")
			if err != nil {
				return fmt.Errorf("resolve config paths: %w", err)
			}

			// Fast-run bootstrap if requested
			if fastRun != "" {
				result, err := bootstrap.ApplyFastRun(paths, fastRun)
				if err != nil {
					return fmt.Errorf("fast-run bootstrap (%s): %w", fastRun, err)
				}
				fmt.Fprintf(os.Stderr, "Imported %q -> active profile: %s (%s)\n",
					fastRun, result.Profile.Name, result.Profile.Model)
			}

			// Load full config
			loaded, err := config.Load("")
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// Build LLM model
			chatModel, err := llm.Build(ctx, loaded.ActiveProfile)
			if err != nil {
				return fmt.Errorf("build LLM: %w", err)
			}

			// Init MCP bridge
			mcpCfg := loaded.App.MCP
			bridge, err := tools.NewStdioBridge(ctx, mcpCfg)
			if err != nil {
				return fmt.Errorf("init MCP: %w", err)
			}
			defer bridge.Close()

			// List available tools
			toolInfos, err := bridge.List(ctx)
			if err != nil {
				return fmt.Errorf("list MCP tools: %w", err)
			}

			// Load prompts
			systemText, err := prompt.Load("system")
			if err != nil {
				return fmt.Errorf("load system prompt: %w", err)
			}
			toolText, err := prompt.Load("tool_use")
			if err != nil {
				return fmt.Errorf("load tool_use prompt: %w", err)
			}

			// Create agent runtime
			rt := agent.NewRuntime(chatModel, bridge, toolInfos, systemText, toolText)

			// Wrap and start TUI
			tuiRuntime := tui.NewRuntimeAdapter(rt)
			model := tui.New(tuiRuntime)
			program := tea.NewProgram(model)

			if _, err := program.Run(); err != nil {
				return fmt.Errorf("TUI run: %w", err)
			}

			return nil
		},
	}

	runCmd.Flags().StringVar(&fastRun, "fast-run", "", "Import profiles from another agent before starting (codex|claude|opencode|kimi)")
	c.AddCommand(runCmd)

	return c
}
