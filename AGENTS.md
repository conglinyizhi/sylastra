# AGENTS

This repository uses the following collaboration rules for future coding agents.

## Commit Messages

When creating git commits, agents must use Conventional Commits and write the
commit message in Chinese.

Examples:

- `feat: 增加 fast-run 导入能力`
- `fix: 修复配置写入时的 TOML 编码问题`
- `docs: 更新 README 中的初始化说明`
- `refactor: 重构配置持久化逻辑`
- `test: 补充 bootstrap 配置回归测试`

Recommended Conventional Commit types:

- `feat`
- `fix`
- `docs`
- `refactor`
- `test`
- `build`
- `ci`
- `chore`

## Scope

If a scope is useful, keep it concise and still write the subject in Chinese.

Examples:

- `feat(config): 支持结构化写入 app.toml`
- `ci(workflows): 增加自动测试工作流`

## General Expectations

- Keep commit subjects concise and action-oriented.
- Do not mix unrelated changes into one commit.
- Prefer one clear logical change per commit.
