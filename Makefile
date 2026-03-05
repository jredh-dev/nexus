# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (c) 2026 Jared Redh. All rights reserved.

BINARY      := tui
INSTALL_DIR := $(shell go env GOPATH)/bin
CMD_DIR     := ./cmd/tui

# Build-time obfuscation (set in CI via secrets; leave empty for dev mode)
OBF_KEY     ?=
OBF_ADDR    ?=
OBF_SECRET  ?=
OBF_SECRETS_URL ?=

LDFLAGS := -X main.obfKey=$(OBF_KEY) \
           -X main.obfAddr=$(OBF_ADDR) \
           -X main.obfSecret=$(OBF_SECRET) \
           -X main.obfSecretsURL=$(OBF_SECRETS_URL)

# Production build values (set in CI via secrets; leave empty for dev mode).
# Used by install-prod to encode + build in one step.
PASSPHRASE       ?=
HERMIT_ADDR_PLAIN    ?=
HERMIT_SECRET_PLAIN  ?=
SECRETS_URL_PLAIN    ?=

ENCODE_CMD := go run ./cmd/tui/internal/obf/encode

.PHONY: build install install-prod test clean encode

## build: compile tui binary to ./bin/tui
build:
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(CMD_DIR)

## install: install tui to GOPATH/bin (pass pre-encoded OBF_* vars, or leave empty for dev)
install:
	go install -ldflags "$(LDFLAGS)" $(CMD_DIR)

## install-prod: encode plaintext values and install with baked obfuscation.
##   Requires: PASSPHRASE, HERMIT_ADDR_PLAIN, HERMIT_SECRET_PLAIN, SECRETS_URL_PLAIN
##   Usage: make install-prod PASSPHRASE=... HERMIT_ADDR_PLAIN=... HERMIT_SECRET_PLAIN=... SECRETS_URL_PLAIN=...
install-prod:
	@test -n "$(PASSPHRASE)" || (echo "error: PASSPHRASE is required" && exit 1)
	@test -n "$(HERMIT_ADDR_PLAIN)" || (echo "error: HERMIT_ADDR_PLAIN is required" && exit 1)
	@test -n "$(HERMIT_SECRET_PLAIN)" || (echo "error: HERMIT_SECRET_PLAIN is required" && exit 1)
	$(eval OBF_KEY := $(shell $(ENCODE_CMD) "$(PASSPHRASE)" "tui"))
	$(eval OBF_ADDR := $(shell $(ENCODE_CMD) "$(HERMIT_ADDR_PLAIN)" "$(PASSPHRASE)"))
	$(eval OBF_SECRET := $(shell $(ENCODE_CMD) "$(HERMIT_SECRET_PLAIN)" "$(PASSPHRASE)"))
	$(eval OBF_SECRETS_URL := $(shell $(ENCODE_CMD) "$(SECRETS_URL_PLAIN)" "$(PASSPHRASE)"))
	go install -ldflags "-X main.obfKey=$(OBF_KEY) -X main.obfAddr=$(OBF_ADDR) -X main.obfSecret=$(OBF_SECRET) -X main.obfSecretsURL=$(OBF_SECRETS_URL)" $(CMD_DIR)

## test: run all tests
test:
	go test ./...

## encode: encode a value for ldflags embedding
##   Usage: make encode PLAIN=<value> PASS=<passphrase>
encode:
	$(ENCODE_CMD) $(PLAIN) $(PASS)

## clean: remove build artifacts
clean:
	rm -rf bin
