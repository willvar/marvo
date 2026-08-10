#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")"
DOCKER_BUILDKIT=1 docker build -t marvo-opencode:local .
docker image prune -f
