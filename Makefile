APP := sylastra
CMD := ./cmd/sylastra
GOCACHE ?= /tmp/go-build

.PHONY: build test clean run config-init config-validate

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
