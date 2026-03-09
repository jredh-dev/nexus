#!/bin/sh
# entrypoint.sh — sources Vault-injected secrets before launching watcher.
#
# vault-agent-opencode writes OPENCODE_PASSWORD (and other secrets) into
# /vault/secrets/watcher.env. We source it here so the binary picks them up.
# If the file doesn't exist (e.g. running without Vault), we fall back to
# whatever OPENCODE_PASSWORD is already set in the environment.
set -e

SECRETS_FILE="/vault/secrets/watcher.env"

if [ -f "$SECRETS_FILE" ]; then
    # shellcheck disable=SC1090
    . "$SECRETS_FILE"
fi

exec /app/watcher "$@"
