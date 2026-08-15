<p align="center">
  <img src="frontend/public/favicon.svg" width="88" height="88" alt="Marvo logo">
</p>

<h1 align="center">Marvo</h1>

<p align="center"><strong>Markdown Revolution</strong></p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-AGPL--3.0--only-1f6feb.svg" alt="AGPL-3.0-only"></a>
</p>

<p align="center"><a href="README.md">中文</a> | English</p>

Marvo is a self-hosted, file-first knowledge workspace. It combines Markdown notes, media, a responsive editing interface, and agents that can work directly inside the workspace, while giving every user a separate data space, device approval flow, and agent runtime.

> Marvo is under active development. Before storing important data, read the [deployment guide](DEPLOY.md) and put a tested, complete backup plan in place.

## Features

- **File-based storage**: each note is an ordinary directory containing `index.md`, `meta.json`, and `assets/`; note content does not depend on database storage.
- **One responsive interface**: desktop, tablet, and phone share the same Vue application, with landscape, portrait, touch, light, and dark mode support.
- **Safe editing**: SHA-256 content revisions, conditional writes, instance tokens, local drafts, and a reviewable three-way merge prevent stale pages from overwriting newer agent or browser changes.
- **Note management**: reading and editing, tags, full-text search, media upload and transcoding, trash, and permanent deletion.
- **Workspace agents**: powered by OpenCode, with independent conversations, attachments, images, live execution progress and file changes, provider connections, model selection, global instructions, and personalization rules.
- **Multi-user isolation**: a platform administrator creates users; every user has isolated notes, media, trash, settings, credentials, conversations, and an agent container.
- **Device approval**: new devices request access and must be approved by the owner of that user space. User administration supports passwords, TOTP, and device revocation.
- **Android app**: the universal APK bundles the frontend and supports QR space binding, Android-native back navigation, sharing, image saving, and in-app updates.

## Architecture

```text
Browser / Marvo Android app
             │
             ▼
       Marvo Go API
       + embedded Vue SPA
          │       │
          │       ├─ control/platform.sqlite
          │       └─ users/<userId>/...
          │
          │ HTTP / SSE + Bearer
          ▼
  marvo-runtime (Docker)
          │ Docker API
          ├─ marvo-agent-<userA> (OpenCode)
          ├─ marvo-agent-<userB> (OpenCode)
          └─ marvo-agent-<userC> (OpenCode)
```

The Marvo service can run as a native process or in a container. The Runtime gateway always runs in Docker and manages a separate Agent container for each user. Agent containers expose no host ports and mount only that user's workspace and Agent Home. Only the constrained Runtime gateway can access the Docker socket.

## Quick start

### Requirements

- Go (see [`go.mod`](go.mod) for the version)
- Node.js and npm
- Docker
- ffmpeg and ffprobe when running Marvo directly on the host

### Start the development environment

```bash
git clone https://github.com/willvar/marvo.git
cd marvo
cp config.example.yaml config.yaml
npm --prefix frontend ci
make dev
```

The first run builds the Agent and Runtime images, waits for the gateway to become healthy, and then starts the Go API and Vite. Default addresses:

| Service         | Address                 |
| --------------- | ----------------------- |
| Web interface   | `http://localhost:5080` |
| Go API          | `http://127.0.0.1:5090` |
| Runtime gateway | `http://127.0.0.1:4097` |

Open `http://localhost:5080/admin/login` and sign in with `auth.password` from `config.yaml`. The sample value, `marvo`, is for loopback development only. A non-default password of at least 12 characters is required when the service listens on a non-loopback address or allows a non-local Origin.

After the platform administrator creates a user, that user's workspace is available at `/user/{userId}`. Platform administration can manage users and legacy migration, but a platform session cannot read user content.

## Common commands

| Command                              | Purpose                                                                      |
| ------------------------------------ | ---------------------------------------------------------------------------- |
| `make dev`                           | Start Go, Vite, and the Runtime development environment                      |
| `make preview`                       | Build the frontend and start the LAN preview environment                     |
| `make stop-runtime`                  | Stop the Runtime gateway and managed Agent containers while preserving state |
| `make build`                         | Build `dist/marvo` with the frontend embedded                                |
| `make test`                          | Run Go and Android unit tests                                                |
| `npm --prefix frontend run test:e2e` | Run responsive browser E2E tests                                             |
| `make test-webkit`                   | Validate portrait flows in Playwright WebKit                                 |
| `make lint`                          | Run Go, frontend, and Android static checks                                  |
| `make audit`                         | Run all formatting, static-analysis, dead-code, test, and build checks       |

## Data storage

All persistent state lives under the configured `server.state_dir`. Containers are disposable; user data is not stored in container writable layers.

```text
<state_dir>/
  control/
    platform.sqlite
    .session-secret
    .runtime-token
    android/
  users/<userId>/
    workspace/
      .devices.json
      .brand.json
      .agent-settings.json
      .agent-personalization.json
      <note title>/
        index.md
        meta.json
        assets/
    agent/home/              # OpenCode sessions, configuration, and credentials
```

Back up the complete `state_dir` as one unit. Databases, OpenCode conversations, user credentials, media, and notes must remain consistent.

## Deployment

Two deployment options are supported:

1. **Marvo as a native process + Docker Runtime**: a simpler service layout with straightforward systemd management.
2. **Fully containerized Marvo**: run Marvo and Runtime with Compose and connect them through Docker DNS.

In both cases, nginx proxies all traffic to one Marvo HTTP port because the Vue SPA is embedded in the Go binary. See the [deployment guide](DEPLOY.md) for complete configuration, systemd, Compose, nginx, backup, and recovery instructions.

## Repository layout

| Path               | Contents                                                                     |
| ------------------ | ---------------------------------------------------------------------------- |
| `cmd/`             | Marvo service and Runtime gateway entry points                               |
| `config/`          | Server configuration loading and validation                                  |
| `internal/`        | Users, authentication, notes, media, agent proxy, and runtime implementation |
| `frontend/`        | Vue 3 frontend, Playwright E2E tests, and Android app                        |
| `docker/opencode/` | OpenCode image used to create per-user Agent containers                      |
| `docker/runtime/`  | Runtime gateway image and local scripts                                      |
| `deploy/`          | systemd and nginx examples                                                   |

## Documentation

- [Development guide](DEVELOPMENT.md) (Chinese)
- [Deployment guide](DEPLOY.md) (Chinese)
- [Android build and native bridge](frontend/android/README.md) (Chinese)
- [Agent Runtime image](docker/opencode/README.md) (Chinese)
- [Contributing](CONTRIBUTING.en.md) ([中文](CONTRIBUTING.md))

## Contributing

Issues, documentation, tests, and code contributions are welcome. For changes involving user isolation, authentication, the on-disk layout, agent runtime boundaries, or critical interactions, open an issue describing the goal and proposed approach first. Read the [contributing guide](CONTRIBUTING.en.md) before submitting a pull request.

## License

Marvo is open source under the [GNU Affero General Public License v3.0 only](LICENSE).

Copyright (C) 2026 William Varmus. Third-party code and assets carrying separate license or copyright notices remain subject to their respective terms.
