# OpenCode Docker Image

This image packages only the OpenCode runtime. Runtime configuration and credentials are not baked into the image.

## Build

```bash
DOCKER_BUILDKIT=1 docker build -t marvo-opencode:local docker/opencode
```

Or from this directory:

```bash
./update.sh
```

The image uses `oven/bun:1-slim` and pins the OpenCode runtime to the same
version as the application SDK. It also includes common tools for Agent-assisted
note and media work: `curl`, `wget`, `git`, `ffmpeg`/`ffprobe`, ImageMagick,
HEIF/WebP tools, ExifTool, Poppler, Python 3, `jq`, `rg`, `file`, and archive
utilities.

OpenCode is installed with:

```bash
bun add -g opencode-ai@1.18.15
```

## Runtime Config

`start.sh` copies the local config into Marvo's host-side state directory before starting the container. The OpenCode config directory and the canonical project `AGENTS.md` are mounted read-only inside the container:

```text
docker/opencode/opencode.json -> $MARVO_OPENCODE_STATE_DIR/home/.config/opencode/opencode.json
docker/opencode/AGENTS.md -> /workspace/AGENTS.md (read-only bind mount)
docker/opencode/bin/marvo-personalization -> /usr/local/bin/marvo-personalization (read-only bind mount)
```

Provider credentials are connected from the Marvo intelligent-agent settings.
OpenCode writes them directly to its persistent Marvo-owned state under
`$MARVO_OPENCODE_STATE_DIR/home/.local/share/opencode/`; `start.sh` never reads
or overwrites the host user's global OpenCode credentials. A fresh deployment
can therefore start before any provider has been connected.

Marvo writes the user-configurable global prompt to the host-side
`$MARVO_OPENCODE_STATE_DIR/home/.config/opencode/AGENTS.md`. OpenCode loads that
global file before `/workspace/AGENTS.md`, so the project rules remain the final
application constraints without a prompt-order plugin. The host server can
atomically update the file while the config directory stays read-only inside
the container.

The read-only `marvo-personalization` helper lets the Agent manage the same
natural-language rule list shown in Marvo settings without exposing the JSON
storage format in prompts or adding runtime package dependencies.

Default state directory:

```text
$HOME/.marvo/opencode-state
```

This means provider/model changes can be made by editing `docker/opencode/opencode.json` and restarting the container. Rebuilding the image is not required.

## Start

```bash
./start.sh
```

Useful environment variables:

```text
MARVO_OPENCODE_IMAGE       default: marvo-opencode:local
MARVO_OPENCODE_PORT        default: 4096
MARVO_DATA_DIR             default: $HOME/.marvo/data
MARVO_OPENCODE_STATE_DIR   default: $HOME/.marvo/opencode-state
```

## Offline Export

```bash
docker save marvo-opencode:local | zstd -T0 -10 -o marvo-opencode-local.tar.zst
```

Import on another host:

```bash
zstd -dc marvo-opencode-local.tar.zst | docker load
```

Then run with the same `start.sh` flow, or run manually with equivalent volume mounts.

## Prompt priority

OpenCode loads the user-configurable global `AGENTS.md` first and the canonical
project `/workspace/AGENTS.md` second. Request-level system text is reserved for
structured Marvo context such as the current note title; user preferences are
not injected there. Editing the canonical project `AGENTS.md` only requires a
container restart because it is mounted directly rather than baked into the
image.
