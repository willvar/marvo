#!/bin/bash
set -euo pipefail

BASE_DIR="$(cd "$(dirname "$0")/../.." && pwd -P)"
STATE_DIR="${MARVO_STATE_DIR:-$HOME/.marvo}"
GATEWAY_IMAGE="${MARVO_RUNTIME_IMAGE:-marvo-runtime:local}"
AGENT_IMAGE="${MARVO_AGENT_IMAGE:-marvo-opencode:local}"
NAME="marvo-runtime"
NETWORK="${MARVO_RUNTIME_NETWORK:-marvo-runtime}"
PORT="${MARVO_RUNTIME_PORT:-4097}"
RUNTIME_UID="${MARVO_RUNTIME_UID:-$(id -u)}"
RUNTIME_GID="${MARVO_RUNTIME_GID:-$(id -g)}"
DOCKER_SOCKET="${MARVO_RUNTIME_DOCKER_SOCKET:-/var/run/docker.sock}"

mkdir -p "$STATE_DIR/control" "$STATE_DIR/users"
STATE_DIR="$(cd "$STATE_DIR" && pwd -P)"
chmod 700 "$STATE_DIR" "$STATE_DIR/control" "$STATE_DIR/users"

if [ ! -S "$DOCKER_SOCKET" ]; then
  echo "Docker socket is unavailable: $DOCKER_SOCKET" >&2
  exit 1
fi
case "$PORT" in ''|*[!0-9]*) echo "MARVO_RUNTIME_PORT must be a port number" >&2; exit 1 ;; esac
case "$RUNTIME_UID:$RUNTIME_GID" in *[!0-9:]*) echo "Runtime UID and GID must be numeric" >&2; exit 1 ;; esac

bash "$BASE_DIR/docker/runtime/images.sh" all
AGENT_GENERATION="$(docker image inspect --format '{{.Id}}' "$AGENT_IMAGE")"
GATEWAY_IMAGE_ID="$(docker image inspect --format '{{.Id}}' "$GATEWAY_IMAGE")"

if ! docker network inspect "$NETWORK" >/dev/null 2>&1; then
  docker network create --driver bridge --label com.marvo.role=runtime-network "$NETWORK" >/dev/null
fi

SOCKET_GID="$(stat -c '%g' "$DOCKER_SOCKET")"
START_SCRIPT_DIGEST="$(sha256sum "$BASE_DIR/docker/runtime/start.sh" | cut -d ' ' -f 1)"
RUNTIME_CONFIG_DIGEST="$(printf '%s\0' \
  'marvo-runtime-container-v1' "$GATEWAY_IMAGE_ID" "$AGENT_IMAGE" "$AGENT_GENERATION" \
  "$STATE_DIR" "$DOCKER_SOCKET" "$SOCKET_GID" "$NETWORK" "$PORT" "$RUNTIME_UID" "$RUNTIME_GID" \
  "$START_SCRIPT_DIGEST" | sha256sum | cut -d ' ' -f 1)"

REUSE_RUNTIME=0
if docker container inspect "$NAME" >/dev/null 2>&1; then
  role="$(docker container inspect --format '{{ index .Config.Labels "com.marvo.role" }}' "$NAME")"
  if [ "$role" != "runtime-gateway" ]; then
    echo "Container name $NAME is occupied by an unmanaged container" >&2
    exit 1
  fi
  existing_config="$(docker container inspect --format '{{ index .Config.Labels "com.marvo.runtime-config" }}' "$NAME")"
  existing_image="$(docker container inspect --format '{{.Image}}' "$NAME")"
  if [ "$existing_config" = "$RUNTIME_CONFIG_DIGEST" ] && [ "$existing_image" = "$GATEWAY_IMAGE_ID" ]; then
    if [ "$(docker container inspect --format '{{.State.Running}}' "$NAME")" != true ]; then
      docker start "$NAME" >/dev/null
    fi
    REUSE_RUNTIME=1
    echo "Runtime gateway unchanged: reusing $NAME"
  else
    docker rm -f "$NAME" >/dev/null
  fi
fi

if [ "$REUSE_RUNTIME" = 0 ]; then
  docker run -d \
    --name "$NAME" \
    --label com.marvo.role=runtime-gateway \
    --label "com.marvo.runtime-network=$NETWORK" \
    --label "com.marvo.runtime-config=$RUNTIME_CONFIG_DIGEST" \
    --restart=unless-stopped \
    --network "$NETWORK" \
    -p "127.0.0.1:$PORT:4097" \
    --user "$RUNTIME_UID:$RUNTIME_GID" \
    --group-add "$SOCKET_GID" \
    --mount "type=bind,src=$DOCKER_SOCKET,dst=/var/run/docker.sock" \
    --mount "type=bind,src=$STATE_DIR,dst=/state" \
    -e MARVO_RUNTIME_LISTEN=0.0.0.0:4097 \
    -e MARVO_RUNTIME_TOKEN_FILE=/state/control/.runtime-token \
    -e MARVO_RUNTIME_STATE_DIR=/state \
    -e "MARVO_RUNTIME_HOST_STATE_DIR=$STATE_DIR" \
    -e "MARVO_RUNTIME_UID=$RUNTIME_UID" \
    -e "MARVO_RUNTIME_GID=$RUNTIME_GID" \
    -e "MARVO_RUNTIME_NETWORK=$NETWORK" \
    -e "MARVO_AGENT_IMAGE=$AGENT_IMAGE" \
    -e "MARVO_AGENT_GENERATION=$AGENT_GENERATION" \
    "$GATEWAY_IMAGE" >/dev/null
fi

echo "Runtime gateway: http://127.0.0.1:$PORT"
echo "State root: $STATE_DIR"
