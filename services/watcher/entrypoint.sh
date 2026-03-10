#!/bin/sh
# entrypoint.sh — sources Vault-injected secrets before launching watcher.
#
# vault-agent-opencode writes OPENCODE_PASSWORD (and other secrets) into
# /vault/secrets/watcher.env. We source it here so the binary picks them up.
# If the file doesn't exist (e.g. running without Vault), we fall back to
# whatever OPENCODE_PASSWORD is already set in the environment.
set -e

# Allow git to operate on the bind-mounted volume regardless of ownership.
# Docker bind mounts from macOS (via the VM) present files owned by a different
# UID than the container user, triggering git's safe.directory check.
# Setting safe.directory=* globally suppresses the check for all paths.
git config --global --add safe.directory '*'

SECRETS_FILE="/vault/secrets/watcher.env"

if [ -f "$SECRETS_FILE" ]; then
    # shellcheck disable=SC1090
    . "$SECRETS_FILE"
    # Export all variables set by the secrets file so exec'd child processes inherit them.
    # POSIX dot-sourcing sets variables in the current shell but does not mark them for
    # export; without explicit export the exec'd binary sees empty values.
    export OPENCODE_PASSWORD
fi

exec /app/watcher "$@"
