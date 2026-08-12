#!/bin/bash
set -euo pipefail

BASE_DIR="$(cd "$(dirname "$0")/../.." && pwd -P)"
TARGET="${1:-all}"
AGENT_IMAGE="${MARVO_AGENT_IMAGE:-marvo-opencode:local}"
GATEWAY_IMAGE="${MARVO_RUNTIME_IMAGE:-marvo-runtime:local}"
FORCE_REBUILD="${MARVO_FORCE_REBUILD:-0}"
SOURCE_LABEL="com.marvo.source-digest"

case "$TARGET" in all | agent | runtime) ;; *) echo "Usage: $0 [all|agent|runtime]" >&2; exit 2 ;; esac
case "$FORCE_REBUILD" in 0 | 1) ;; *) echo "MARVO_FORCE_REBUILD must be 0 or 1" >&2; exit 2 ;; esac
command -v sha256sum >/dev/null 2>&1 || { echo "sha256sum is required to fingerprint image sources" >&2; exit 1; }

source_digest() {
  local digest
  digest="$({
    cd "$BASE_DIR"
    find "$@" -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum
  } | sha256sum | cut -d ' ' -f 1)"
  case "$digest" in (*[!0-9a-f]* | '') echo "failed to fingerprint image sources" >&2; exit 1 ;; esac
  [ "${#digest}" -eq 64 ] || { echo "invalid image source fingerprint" >&2; exit 1; }
  printf '%s\n' "$digest"
}

current_source_digest() {
  docker image inspect --format "{{ index .Config.Labels \"$SOURCE_LABEL\" }}" "$1" 2>/dev/null || true
}

build_image() {
  local image="$1"
  local digest="$2"
  shift 2
  local current
  current="$(current_source_digest "$image")"
  if [ "$FORCE_REBUILD" = 0 ] && [ "$current" = "$digest" ]; then
    echo "Docker image unchanged: $image"
    return
  fi

  local flags=()
  if [ "$FORCE_REBUILD" = 1 ]; then
    flags+=(--pull --no-cache)
  fi
  DOCKER_BUILDKIT=1 docker build "${flags[@]}" --label "$SOURCE_LABEL=$digest" -t "$image" "$@"
}

ensure_agent_image() {
  if [ "$AGENT_IMAGE" != "marvo-opencode:local" ]; then
    docker image inspect "$AGENT_IMAGE" >/dev/null 2>&1 || { echo "Agent image not found: $AGENT_IMAGE" >&2; exit 1; }
    return
  fi
  local digest
  digest="$(source_digest \
    docker/opencode/Dockerfile \
    docker/opencode/AGENTS.md \
    docker/opencode/opencode.json \
    docker/opencode/entrypoint.sh \
    docker/opencode/bin)"
  build_image "$AGENT_IMAGE" "$digest" "$BASE_DIR/docker/opencode"
}

ensure_runtime_image() {
  if [ "$GATEWAY_IMAGE" != "marvo-runtime:local" ]; then
    docker image inspect "$GATEWAY_IMAGE" >/dev/null 2>&1 || { echo "Runtime gateway image not found: $GATEWAY_IMAGE" >&2; exit 1; }
    return
  fi
  local digest
  digest="$(source_digest \
    docker/runtime/Dockerfile \
    docker/runtime/go.mod \
    docker/runtime/healthcheck.sh \
    cmd/marvo-runtime \
    internal/agentcredentials \
    internal/runtimeauth \
    internal/runtimegateway \
    internal/userid)"
  build_image "$GATEWAY_IMAGE" "$digest" -f "$BASE_DIR/docker/runtime/Dockerfile" "$BASE_DIR"
}

if [ "$TARGET" = all ] || [ "$TARGET" = agent ]; then
  ensure_agent_image
fi
if [ "$TARGET" = all ] || [ "$TARGET" = runtime ]; then
  ensure_runtime_image
fi
