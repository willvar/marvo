#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(cd "$FRONTEND_DIR/.." && pwd)"
DATA_DIR="$SCRIPT_DIR/.data"
IMAGE="${PLAYWRIGHT_DOCKER_IMAGE:-mcr.microsoft.com/playwright:v1.62.1-noble}"
TEMP_DIR="$(mktemp -d)"
USER_ID="$(id -u)"
GROUP_ID="$(id -g)"

cleanup_temp() {
  case "$TEMP_DIR" in
    /tmp/tmp.*) ;;
    *) return ;;
  esac
  rm -f -- "$TEMP_DIR/marvo" "$TEMP_DIR/fakeopencode"
  rmdir -- "$TEMP_DIR" 2>/dev/null || true
}
trap cleanup_temp EXIT

if [[ -e "$DATA_DIR" ]]; then
  if [[ -L "$DATA_DIR" || "$(realpath -- "$DATA_DIR")" != "$DATA_DIR" ]]; then
    echo "Refusing to reset unexpected WebKit data path: $DATA_DIR" >&2
    exit 1
  fi
  if find "$DATA_DIR" -type l -print -quit | grep -q .; then
    echo "Refusing to reset WebKit data containing symbolic links: $DATA_DIR" >&2
    exit 1
  fi
  find "$DATA_DIR" -mindepth 1 -depth -delete
else
  mkdir -p "$DATA_DIR"
fi

CGO_ENABLED=0 go build -o "$TEMP_DIR/marvo" "$REPO_DIR"
CGO_ENABLED=0 go build -o "$TEMP_DIR/fakeopencode" "$REPO_DIR/testsupport/fakeopencode"

if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  docker pull "$IMAGE"
fi

docker run --rm -i \
  --ipc=host \
  --network=host \
  --user "$USER_ID:$GROUP_ID" \
  -e HOME=/tmp/marvo-home \
  -v "$REPO_DIR:/work" \
  -v "$TEMP_DIR:/opt/marvo-test:ro" \
  -w /work \
  "$IMAGE" bash -s -- "$@" <<'CONTAINER_SCRIPT'
set -euo pipefail

mkdir -p "$HOME"
fake_pid=""
server_pid=""
vite_pid=""

cleanup_services() {
  [[ -z "$vite_pid" ]] || kill "$vite_pid" 2>/dev/null || true
  [[ -z "$server_pid" ]] || kill "$server_pid" 2>/dev/null || true
  [[ -z "$fake_pid" ]] || kill "$fake_pid" 2>/dev/null || true
}
trap cleanup_services EXIT

MARVO_FAKE_OPENCODE_ADDR=127.0.0.1:15096 /opt/marvo-test/fakeopencode >/tmp/marvo-fake.log 2>&1 &
fake_pid=$!
/opt/marvo-test/marvo -c frontend/e2e/config.yaml >/tmp/marvo-api.log 2>&1 &
server_pid=$!
VITE_API_TARGET=http://127.0.0.1:15090 npm --prefix frontend run dev -- --host 127.0.0.1 --port 15080 >/tmp/marvo-vite.log 2>&1 &
vite_pid=$!

for _attempt in $(seq 1 120); do
  if curl -fs http://127.0.0.1:15096/config >/dev/null \
    && curl -fs http://127.0.0.1:15090/api/health >/dev/null \
    && curl -fs http://127.0.0.1:15080/ >/dev/null; then
    break
  fi
  sleep 0.25
done

if ! curl -fsS http://127.0.0.1:15096/config >/dev/null \
  || ! curl -fsS http://127.0.0.1:15090/api/health >/dev/null \
  || ! curl -fsS http://127.0.0.1:15080/ >/dev/null; then
  tail -80 /tmp/marvo-fake.log /tmp/marvo-api.log /tmp/marvo-vite.log
  exit 1
fi

cd /work/frontend
if [[ $# -eq 0 ]]; then
  set -- e2e/core-flow.spec.ts e2e/auth.spec.ts
fi
E2E_REUSE_SERVERS=1 npx playwright test "$@" --project=webkit-portrait
CONTAINER_SCRIPT
