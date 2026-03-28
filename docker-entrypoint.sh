#!/bin/sh
set -e

DATA_DIR="${MAXIOFS_DATA_DIR:-/data}"

# Parse --data-dir flag if passed explicitly
prev=""
for arg in "$@"; do
    case "$prev" in
        --data-dir) DATA_DIR="$arg" ;;
    esac
    prev="$arg"
done

# Create the data directory if it doesn't exist
mkdir -p "$DATA_DIR"

# If running as root, fix ownership then drop privileges to the maxiofs user
if [ "$(id -u)" = "0" ]; then
    chown -R maxiofs:maxiofs "$DATA_DIR"
    exec runuser -u maxiofs -- "$0" "$@"
fi

# On first run, create config.yaml in the data directory from the bundled example.
# The user can then edit this file and restart the container to apply changes.
CONFIG_FILE="${DATA_DIR}/config.yaml"
if [ ! -f "$CONFIG_FILE" ]; then
    echo "First run: creating ${CONFIG_FILE} from example..."
    cp /app/config.example.yaml "$CONFIG_FILE"
    # Set data_dir to the actual data directory
    sed -i "s|data_dir:.*|data_dir: \"${DATA_DIR}\"|" "$CONFIG_FILE"
    echo "Edit ${CONFIG_FILE} to configure encryption, SMTP, public URLs, and other settings."
    echo "Then restart the container to apply changes."
fi

exec /app/maxiofs --config "$CONFIG_FILE" "$@"
