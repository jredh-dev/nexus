#!/bin/sh
# entrypoint.sh — Source secrets then exec deadman.
#
# Secrets can come from two sources depending on the runtime environment:
#
#   Local (docker-compose + Vault Agent sidecar):
#     Vault Agent renders /vault/secrets/secrets.env on a shared tmpfs. We wait
#     up to VAULT_SECRETS_TIMEOUT seconds for it to appear, then source it.
#     Set VAULT_SECRETS_TIMEOUT=0 to skip the wait (Cloud Run, local dev w/o Vault).
#
#   Cloud Run:
#     Secrets are injected directly as env vars via --update-secrets. No Vault.
#     VAULT_SECRETS_TIMEOUT defaults to 0 so we skip the wait entirely.
#     DATABASE_URL is built from CLOUD_SQL_CONNECTION_NAME (Unix socket via
#     Cloud SQL Auth Proxy, which Cloud Run provides natively).
#
# DATABASE_URL priority (highest to lowest):
#   1. DATABASE_URL already set in environment (explicit override)
#   2. CLOUD_SQL_CONNECTION_NAME set → Unix socket DSN (Cloud Run)
#   3. DEADMAN_PG_HOST set → TCP DSN (docker-compose / local)
#   4. Default local dev DSN (host=postgres)

SECRETS_FILE="/vault/secrets/secrets.env"
# Default to 0 on Cloud Run (no Vault Agent sidecar). Override to 30 in
# docker-compose for local Vault Agent support.
VAULT_SECRETS_TIMEOUT="${VAULT_SECRETS_TIMEOUT:-0}"

# Wait for Vault Agent to render the secrets file (local only).
if [ "$VAULT_SECRETS_TIMEOUT" -gt 0 ]; then
    _waited=0
    while [ ! -f "$SECRETS_FILE" ] && [ "$_waited" -lt "$VAULT_SECRETS_TIMEOUT" ]; do
        echo "[entrypoint] waiting for $SECRETS_FILE (${_waited}s / ${VAULT_SECRETS_TIMEOUT}s)..."
        sleep 2
        _waited=$((_waited + 2))
    done
fi

if [ -f "$SECRETS_FILE" ]; then
    # Export each KEY=VALUE line; skip blank lines and comments.
    # Using set -a / set +a is the portable POSIX way to export all sourced vars.
    set -a
    # shellcheck disable=SC1090
    . "$SECRETS_FILE"
    set +a
    echo "[entrypoint] secrets loaded from $SECRETS_FILE"
else
    echo "[entrypoint] no Vault secrets file — using injected env vars"
fi

# Build DATABASE_URL if not already set in the environment.
# This runs AFTER sourcing secrets.env so DEADMAN_DB_PASSWORD is available.
if [ -z "${DATABASE_URL:-}" ]; then
    if [ -n "${CLOUD_SQL_CONNECTION_NAME:-}" ]; then
        # Cloud Run: connect via Cloud SQL Auth Proxy Unix socket.
        # The proxy socket lives at /cloudsql/<connection-name>/.s.PGSQL.5432.
        # DEADMAN_DB_PASSWORD is injected via --update-secrets.
        _pass="${DEADMAN_DB_PASSWORD:-}"
        DATABASE_URL="host=/cloudsql/${CLOUD_SQL_CONNECTION_NAME} dbname=deadman user=deadman password=${_pass} sslmode=disable"
        export DATABASE_URL
        echo "[entrypoint] DATABASE_URL set via Cloud SQL socket (${CLOUD_SQL_CONNECTION_NAME})"
    else
        # Local docker-compose: connect to the postgres service over TCP.
        _host="${DEADMAN_PG_HOST:-postgres}"
        _pass="${DEADMAN_DB_PASSWORD:-deadman-dev-password}"
        DATABASE_URL="host=${_host} dbname=deadman user=deadman password=${_pass} sslmode=disable"
        export DATABASE_URL
        echo "[entrypoint] DATABASE_URL constructed from env (host=${_host})"
    fi
fi

exec /deadman "$@"
