[English](README.md)

# Sylastra

一个小型的 Go TUI coding agent，运行时依赖：

- 本地配置目录
- 一个当前激活的 LLM profile
- 一个通过 stdio 接入的 MCP server

## 依赖

- Go 1.24+
- LLM API Key
- `better-edit-tools` 或其他兼容的 MCP server 二进制，并且要能从 `PATH` 找到

## 快速开始

先编译：

```bash
go build -buildvcs=false ./cmd/sylastra
```

或者直接：

```bash
make build
```

生成示例配置：

```bash
./sylastra config init
```

会生成：

- `~/.config/sylastra/llms.toml`
- `~/.config/sylastra/llm.index.toml`
- `~/.config/sylastra/app.toml`

`config init` 不会自动下载 MCP 工具。默认会写入：

```toml
[mcp]
command = "better-edit-tools"
args = ["--lang", "zh"]

[mcp.fallback]
enabled = true
```

MCP 命令解析顺序是：

1. `app.toml` 里的 `mcp.command`
2. fallback 开启时的 `~/.local/sylastra/mcp/bin/better-edit-tools`
3. 其他本地 agent 配置里已经明确指向的 `better-edit-tools`，例如 `codex`、`claude`、`opencode`、`kimi`
4. `PATH` 里的 `better-edit-tools`

你可以把二进制安装到 `PATH`，放到 `~/.local/sylastra/mcp/bin/`，让其他本地 agent 配置指向它，或者手动把 `command` 改成绝对路径。

检查本地环境时可以用：

```bash
make doctor
make mcp-path
```

校验配置：

```bash
./sylastra config validate
```

启动 TUI：

```bash
./sylastra tui run
```

快速初始化：

```bash
./sylastra config init --first-run "sk-xxx,gpt-4o"
./sylastra config init --fast-run codex
./sylastra tui run --fast-run claude
```

## 配置 LLM

编辑 `~/.config/sylastra/llms.toml`，至少保留一个 profile。

支持的 `api_style`：

- `openai_chat`
- `openai_responses`
- `anthropic_messages`

最小示例：

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

然后在 `~/.config/sylastra/llm.index.toml` 里设置当前 profile：

```toml
active = "example-openai"
```

## 配置 MCP Server

编辑 `~/.config/sylastra/app.toml`：

```toml
[mcp]
command = "better-edit-tools"
args = ["--lang", "zh"]

[mcp.fallback]
enabled = true
```

如果二进制不在 `PATH` 里，就改成绝对路径。

## 常用命令

```bash
./sylastra config init
./sylastra config init --first-run "sk-xxx,gpt-4o[,https://api.openai.com/v1]"
./sylastra config init --fast-run codex
./sylastra config validate
./sylastra config show-active
./sylastra tui run
./sylastra tui run --fast-run claude
```

## 说明

- `config init` 默认只写一次，想覆盖可加 `--force`。
- GitHub Actions 手动构建会同时产出纯净二进制 artifact 和一个已经包含 `mcp/bin/better-edit-tools` 的 bundle artifact。
- bundle artifact 还会附带一个 `BUNDLE_INFO.txt`，里面会写清楚内置 `better-edit-tools` 的版本号和原始下载来源。
- `--first-run` 可以用一段紧凑输入直接写出可用的 Sylastra 配置，并把初始化元信息保存到 `app.toml`。
- `--first-run` 默认通过 `OPENAI_API_KEY`、`ANTHROPIC_API_KEY` 这类环境变量名引用密钥，不会把密钥明文写进 `llms.toml`。
- `--fast-run` 可以从 `codex`、`claude`、`opencode`、`kimi` 导入配置，并写入 Sylastra 自己的配置文件。
- `--fast-run` 和 `--first-run` 当前都会替换现有的 `profiles` 列表与 active profile。
- 如果你不想使用 `~/.local/sylastra/mcp/bin/` 这条 fallback 路径，可以把 `[mcp.fallback].enabled = false`。
- Sylastra 不会自动下载 MCP 二进制，安装动作保持显式，失败原因也更清楚。
- 对本地源码构建来说，推荐的 fallback 放置路径就是 `~/.local/sylastra/mcp/bin/better-edit-tools`。
- 当前 TUI 只有一个内存会话，不做持久化。
- Prompt 文件嵌入自 `prompts/zh/`。
