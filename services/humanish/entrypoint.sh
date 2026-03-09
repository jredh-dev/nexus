#!/bin/sh
# entrypoint.sh — sources Vault-injected secrets before launching humanish.
#
# vault-agent-opencode writes OPENCODE_PASSWORD (and other secrets) into
# /vault/secrets/humanish.env. We source it here so the binary picks them up.
# If the file doesn't exist (e.g. running without Vault), we fall back to
# whatever OPENCODE_PASSWORD is already set in the environment.
set -e

SECRETS_FILE="/vault/secrets/humanish.env"

if [ -f "$SECRETS_FILE" ]; then
    # set -a auto-exports all variables defined during source,
    # ensuring they are inherited by the exec'd binary.
    set -a
    # shellcheck disable=SC1090
    . "$SECRETS_FILE"
    set +a
fi

exec /app/humanish "$@"
