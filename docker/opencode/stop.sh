#!/bin/bash
set -euo pipefail

NAME="marvo-opencode"

if docker ps -a --format '{{.Names}}' | grep -Fxq "$NAME"; then
  docker rm -f "$NAME"
  echo "Stopped OpenCode container: $NAME"
else
  echo "OpenCode container is not running: $NAME"
fi
