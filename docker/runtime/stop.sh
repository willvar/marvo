#!/bin/bash
set -euo pipefail

NAME="marvo-runtime"
NETWORK="${MARVO_RUNTIME_NETWORK:-marvo-runtime}"
if ! docker container inspect "$NAME" >/dev/null 2>&1; then
  echo "Runtime gateway is not running"
  exit 0
fi
role="$(docker container inspect --format '{{ index .Config.Labels "com.marvo.role" }}' "$NAME")"
if [ "$role" != "runtime-gateway" ]; then
  echo "Refusing to remove unmanaged container named $NAME" >&2
  exit 1
fi
docker rm -f "$NAME" >/dev/null
while IFS= read -r agent_id; do
  [ -n "$agent_id" ] || continue
  docker stop "$agent_id" >/dev/null
done < <(docker ps -q --filter label=com.marvo.role=agent --filter "label=com.marvo.runtime-network=$NETWORK")
echo "Runtime gateway and managed user Agent runtimes stopped; containers and persistent data were preserved"
