APP := sylastra
CMD := ./cmd/sylastra
GOCACHE ?= /tmp/go-build
LOCAL_MCP_PATH := $(HOME)/.local/sylastra/mcp/bin/better-edit-tools

.PHONY: build test clean run config-init config-validate doctor mcp-path

build:
	GOCACHE=$(GOCACHE) go build -buildvcs=false -o $(APP) $(CMD)

test:
	GOCACHE=$(GOCACHE) go test ./...

clean:
	rm -f $(APP) gotui-agent

run: build
	./$(APP) tui run

config-init: build
	./$(APP) config init

config-validate: build
	./$(APP) config validate

doctor: build
	@printf 'Sylastra binary: %s/%s\n' "$$(pwd)" "$(APP)"
	@if [ -x "$(LOCAL_MCP_PATH)" ]; then \
		printf 'MCP fallback binary: %s\n' "$(LOCAL_MCP_PATH)"; \
	elif command -v better-edit-tools >/dev/null 2>&1; then \
		printf 'MCP from PATH: %s\n' "$$(command -v better-edit-tools)"; \
	else \
		printf 'MCP not found.\n'; \
		printf 'Expected one of:\n'; \
		printf '  1. %s\n' "$(LOCAL_MCP_PATH)"; \
		printf '  2. better-edit-tools in PATH\n'; \
		printf '  3. ~/.config/sylastra/app.toml with an absolute mcp.command\n'; \
		exit 1; \
	fi

mcp-path:
	@printf '%s\n' "$(LOCAL_MCP_PATH)"
