#!/bin/bash
set -euo pipefail

BASE_DIR="$(cd "$(dirname "$0")" && pwd)"
IMAGE="${MARVO_OPENCODE_IMAGE:-marvo-opencode:local}"
NAME="marvo-opencode"
PORT="${MARVO_OPENCODE_PORT:-4096}"
DATADIR="${MARVO_DATA_DIR:-$HOME/.marvo/data}"
USER_ID="$(id -u)"
GROUP_ID="$(id -g)"
USER_NAME="$(id -un)"
STATE_DIR="${MARVO_OPENCODE_STATE_DIR:-$HOME/.marvo/opencode-state}"
OPENCODE_HOME="$STATE_DIR/home"
OPENCODE_CONFIG_DIR="$OPENCODE_HOME/.config/opencode"
CONTAINER_HOME="/home/$USER_NAME"

mkdir -p "$DATADIR"
mkdir -p "$OPENCODE_CONFIG_DIR" "$OPENCODE_HOME/.local/share/opencode"
chmod 700 "$DATADIR" "$STATE_DIR" "$OPENCODE_HOME" "$OPENCODE_HOME/.config" "$OPENCODE_CONFIG_DIR" "$OPENCODE_HOME/.local" "$OPENCODE_HOME/.local/share" "$OPENCODE_HOME/.local/share/opencode"
install -m 600 "$BASE_DIR/AGENTS.md" "$DATADIR/AGENTS.md"
install -m 600 "$BASE_DIR/opencode.json" "$OPENCODE_CONFIG_DIR/opencode.json"

ENV_ARGS=()
ENV_ARGS+=(-e OPENCODE_ENABLE_EXA=1)
if [ -n "${EXA_API_KEY:-}" ]; then
  ENV_ARGS+=(-e "EXA_API_KEY=$EXA_API_KEY")
fi

if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  if [ "$IMAGE" != "marvo-opencode:local" ]; then
    echo "Docker image not found: $IMAGE" >&2
    echo "Set MARVO_OPENCODE_IMAGE to an existing image, or build it first." >&2
    exit 1
  fi

  echo "Docker image not found: $IMAGE"
  echo "Building local OpenCode image..."
  DOCKER_BUILDKIT=1 docker build -t "$IMAGE" "$BASE_DIR"
fi

docker rm -f "$NAME" 2>/dev/null || true
docker run -d \
  --name "$NAME" \
  --restart=unless-stopped \
  -p "127.0.0.1:$PORT:4096" \
  --user "$USER_ID:$GROUP_ID" \
  -e TZ=Asia/Hong_Kong \
  -e HOME="$CONTAINER_HOME" \
  -v /etc/localtime:/etc/localtime:ro \
  -v "$OPENCODE_HOME:$CONTAINER_HOME" \
  --mount "type=bind,src=$OPENCODE_CONFIG_DIR,dst=$CONTAINER_HOME/.config/opencode,readonly" \
  -v "$DATADIR:/workspace" \
  --mount "type=bind,src=$BASE_DIR/AGENTS.md,dst=/workspace/AGENTS.md,readonly" \
  -w /workspace \
  "${ENV_ARGS[@]}" \
  "$IMAGE"

echo "OpenCode server: http://127.0.0.1:$PORT"
echo "Workspace: $DATADIR"
echo "OpenCode home: $OPENCODE_HOME"
