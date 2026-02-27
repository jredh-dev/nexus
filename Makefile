# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (c) 2026 Jared Redh. All rights reserved.

BINARY      := client-tui
INSTALL_DIR := $(shell go env GOPATH)/bin
CMD_DIR     := ./cmd/client-tui

# Build-time obfuscation (set in CI via secrets; leave empty for dev mode)
OBF_KEY     ?=
OBF_ADDR    ?=
OBF_SECRET  ?=
OBF_SECRETS_URL ?=

LDFLAGS := -X main.obfKey=$(OBF_KEY) \
           -X main.obfAddr=$(OBF_ADDR) \
           -X main.obfSecret=$(OBF_SECRET) \
           -X main.obfSecretsURL=$(OBF_SECRETS_URL)

.PHONY: build install test clean encode

## build: compile client-tui binary to ./bin/client-tui
build:
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(CMD_DIR)

## install: install client-tui to GOPATH/bin
install:
	go install -ldflags "$(LDFLAGS)" $(CMD_DIR)

## test: run all tests
test:
	go test ./...

## encode: encode a value for ldflags embedding
##   Usage: make encode PLAIN=<value> PASS=<passphrase>
encode:
	go run ./cmd/client-tui/internal/obf/encode $(PLAIN) $(PASS)

## clean: remove build artifacts
clean:
	rm -rf bin
