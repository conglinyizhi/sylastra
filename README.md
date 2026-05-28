[中文](README.zh.md)

# Sylastra

A small Go TUI coding agent that runs with:

- a local config directory
- one active LLM profile
- one MCP server over stdio

## Requirements

- Go 1.24+
- an LLM API key
- `better-edit-tools` or another compatible MCP server binary available in `PATH`

## Quick Start

Build the binary:

```bash
go build -buildvcs=false ./cmd/sylastra
```

Create example config files:

```bash
./sylastra config init
```

This creates:

- `~/.config/sylastra/llms.toml`
- `~/.config/sylastra/llm.index.toml`
- `~/.config/sylastra/app.toml`

`config init` does not download MCP tools automatically. By default it writes:

```toml
[mcp]
command = "better-edit-tools"
args = ["--lang", "zh"]

[mcp.fallback]
enabled = true
```

MCP command resolution order is:

1. `mcp.command` from `app.toml`
2. `~/.local/sylastra/mcp/bin/better-edit-tools` when fallback is enabled
3. `better-edit-tools` from `PATH`

Install the binary into `PATH`, place it under `~/.local/sylastra/mcp/bin/`, or replace `command` with an absolute path yourself.

Validate the config:

```bash
./sylastra config validate
```

Run the TUI:

```bash
./sylastra tui run
```

Quick bootstrap:

```bash
./sylastra config init --first-run "sk-xxx,gpt-4o"
./sylastra config init --fast-run codex
./sylastra tui run --fast-run claude
```

## Configure The LLM

Edit `~/.config/sylastra/llms.toml` and set one profile.

Supported `api_style` values:

- `openai_chat`
- `openai_responses`
- `anthropic_messages`

Minimal example:

```toml
[[profiles]]
name = "example-openai"
api_style = "openai_chat"
base_url = "https://api.openai.com/v1"
model = "gpt-4.1-mini"
api_key_env = "OPENAI_API_KEY"
timeout = 120
max_tokens = 2048
temperature = 0.2
```

Then choose the active profile in `~/.config/sylastra/llm.index.toml`:

```toml
active = "example-openai"
```

## Configure The MCP Server

Edit `~/.config/sylastra/app.toml`:

```toml
[mcp]
command = "better-edit-tools"
args = ["--lang", "en"]

[mcp.fallback]
enabled = true
```

If the binary is not in `PATH`, use an absolute path instead.

## Useful Commands

```bash
./sylastra config init
./sylastra config init --first-run "sk-xxx,gpt-4o[,https://api.openai.com/v1]"
./sylastra config init --fast-run codex
./sylastra config validate
./sylastra config show-active
./sylastra tui run
./sylastra tui run --fast-run claude
```

## Notes

- `config init` writes example files only once. Use `--force` to overwrite them.
- The manual GitHub Actions build publishes both a plain binary artifact and a bundled artifact that already includes `mcp/bin/better-edit-tools`.
- `--first-run` writes a usable Sylastra config from a compact input string and stores bootstrap metadata in `app.toml`.
- `--first-run` stores API keys through environment variable names such as `OPENAI_API_KEY` or `ANTHROPIC_API_KEY`; it does not write the key inline to `llms.toml`.
- `--fast-run` imports settings from `codex`, `claude`, `opencode`, or `kimi`, then writes the imported result into Sylastra's own config files.
- `--fast-run` and `--first-run` currently replace the existing `profiles` list and active profile with the imported or detected one.
- Set `[mcp.fallback].enabled = false` if you want to disable the built-in fallback lookup under `~/.local/sylastra/mcp/bin/`.
- Sylastra does not auto-download MCP binaries. Install them explicitly so failure modes stay predictable.
- The TUI keeps one in-memory session only.
- Prompt files are embedded from `prompts/zh/`.
