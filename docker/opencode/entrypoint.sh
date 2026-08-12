#!/bin/sh
set -eu

config_dir="$HOME/.config/opencode"
config_file="$config_dir/opencode.json"
mkdir -p "$config_dir" "$HOME/.local/share/opencode"

if ! cmp -s /opt/marvo/opencode.json "$config_file" 2>/dev/null; then
  if ! cp /opt/marvo/opencode.json "$config_file" 2>/dev/null; then
    if [ ! -r "$config_file" ]; then
      echo "Unable to initialize OpenCode configuration" >&2
      exit 1
    fi
  else
    chmod 600 "$config_file"
  fi
fi

# New multi-user runtimes use an immutable in-image target. The legacy launcher
# may already have a read-only bind at this path, in which case it stays intact.
if rm -f /workspace/AGENTS.md 2>/dev/null; then
  ln -s /opt/marvo/AGENTS.md /workspace/AGENTS.md
elif [ ! -r /workspace/AGENTS.md ]; then
  echo "Unable to expose canonical Agent instructions" >&2
  exit 1
fi

exec "$@"
