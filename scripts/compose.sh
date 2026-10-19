#!/usr/bin/env sh
set -eu

# Docker Desktop does not always put its CLI on PATH on macOS. Resolve it once.
if [ -z "${DOCKER:-}" ]; then
  if command -v docker >/dev/null 2>&1; then
    DOCKER=docker
  elif [ -x /Applications/Docker.app/Contents/Resources/bin/docker ]; then
    DOCKER=/Applications/Docker.app/Contents/Resources/bin/docker
    # The credential helper and other Docker Desktop tools live next to the CLI.
    PATH="/Applications/Docker.app/Contents/Resources/bin:$PATH"
    export PATH
  else
    echo "docker CLI not found. Install Docker Desktop or put docker on PATH." >&2
    exit 1
  fi
fi

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec "$DOCKER" compose -f "$SCRIPT_DIR/../infra/compose.yaml" "$@"
