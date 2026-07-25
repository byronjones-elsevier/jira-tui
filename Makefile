BIN     := jira-tui
INSTALL := $(HOME)/.local/bin/$(BIN)

.PHONY: all build test lint clean install run help

all: build

## build: compile the binary
build:
	go build -o $(BIN) .

## test: run all unit tests
test:
	go test ./...

## lint: vet + shellcheck on bash scripts
lint:
	go vet ./...
	shellcheck *.sh

## clean: remove the compiled binary
clean:
	rm -f $(BIN)

## install: build and copy binary to ~/.local/bin
install: build
	mkdir -p $(dir $(INSTALL))
	cp $(BIN) $(INSTALL)

## run: build and launch the TUI
run: build
	./$(BIN)

## help: list available targets
help:
	@grep -E '^## ' Makefile | sed 's/^## //' | column -t -s ':'
