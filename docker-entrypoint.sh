#!/bin/sh
set -e

# Resolve the data directory from env var or first --data-dir flag argument
DATA_DIR="${MAXIOFS_DATA_DIR:-/data}"
for arg in "$@"; do
    case "$prev" in
        --data-dir) DATA_DIR="$arg" ;;
    esac
    prev="$arg"
done

# Create the data directory if it doesn't exist.
# This handles the case where the user mounts a non-existent host path.
mkdir -p "$DATA_DIR"

# If running as root (default in most Docker setups), fix ownership of the
# data directory then drop privileges to the maxiofs user.
# This makes both named volumes and bind mounts work regardless of the host
# directory ownership.
if [ "$(id -u)" = "0" ]; then
    chown -R maxiofs:maxiofs "$DATA_DIR"
    exec gosu maxiofs /app/maxiofs "$@"
fi

# Already running as a non-root user (e.g. --user flag passed to docker run).
# Proceed directly — the caller is responsible for ensuring correct permissions.
exec /app/maxiofs "$@"
