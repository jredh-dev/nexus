#!/bin/sh
# entrypoint.sh — Source Vault Agent rendered secrets then exec deadman.
#
# The Vault Agent sidecar (vault-agent-deadman) renders secrets from Vault KV
# into /vault/secrets/secrets.env on a shared tmpfs volume. This script sources
# that file to inject the secrets into the process environment before handing
# off to the deadman binary.
#
# If the secrets file is absent (e.g. local dev without Vault), deadman starts
# with whatever environment variables were passed by docker-compose. This allows
# the DEADMAN_DB_PASSWORD fallback in DATABASE_URL to still work.

SECRETS_FILE="/vault/secrets/secrets.env"

if [ -f "$SECRETS_FILE" ]; then
    # Export each KEY=VALUE line; skip blank lines and comments.
    # Using set -a / set +a is the portable POSIX way to export all sourced vars.
    set -a
    # shellcheck disable=SC1090
    . "$SECRETS_FILE"
    set +a
    echo "[entrypoint] secrets loaded from $SECRETS_FILE"
else
    echo "[entrypoint] $SECRETS_FILE not found — starting without Vault secrets"
fi

# Build DATABASE_URL from the Vault-rendered DEADMAN_DB_PASSWORD if not already
# set. DEADMAN_PG_HOST defaults to "postgres" (docker-compose service name).
# This runs AFTER sourcing secrets.env so DEADMAN_DB_PASSWORD is available.
if [ -z "${DATABASE_URL:-}" ]; then
    _host="${DEADMAN_PG_HOST:-postgres}"
    _pass="${DEADMAN_DB_PASSWORD:-deadman-dev-password}"
    DATABASE_URL="host=${_host} dbname=deadman user=deadman password=${_pass} sslmode=disable"
    export DATABASE_URL
    echo "[entrypoint] DATABASE_URL constructed from env"
fi

exec /deadman "$@"
