# Contributing

[中文](CONTRIBUTING.md) | English

Thank you for improving Marvo. Bug reports, interaction suggestions, documentation, tests, and code contributions are all welcome.

Marvo spans user data, authentication, the filesystem, a Docker Runtime, a browser interface, and an Android WebView. Making a change easy to verify is as important as implementing the feature itself.

## Before you start

- Small bug fixes, tests, and documentation improvements can go directly to a pull request.
- Open an issue first for new features, on-disk format changes, authentication changes, dependency replacements, or proposals that materially change an interaction.
- Do not paste real passwords, cookies, tokens, provider keys, user content, or complete state directories into public issues, especially for security reports.
- Never commit local configuration, production data, signing material, or generated artifacts.

## Development environment

Install:

- Go (see [`go.mod`](go.mod) for the version)
- Node.js and npm
- Docker
- ffmpeg and ffprobe
- JDK 17 or 21 and Android SDK 36 when changing Android code

Initialize and start the project:

```bash
git clone https://github.com/willvar/marvo.git
cd marvo
cp config.example.yaml config.yaml
npm --prefix frontend ci
make dev
```

See the [development guide](DEVELOPMENT.md) for the complete runtime topology, ports, initial authorization flow, and on-disk layout.

## Repository structure

| Path                       | Responsibility                                                 |
| -------------------------- | -------------------------------------------------------------- |
| `cmd/`                     | Marvo service and Runtime gateway entry points                 |
| `config/`                  | Configuration defaults, path resolution, and safety validation |
| `internal/control/`        | Platform users, passwords, TOTP, and the control database      |
| `internal/store/`          | File-backed notes, devices, branding, and agent settings       |
| `internal/handler/`        | HTTP APIs, authentication boundaries, and the OpenCode proxy   |
| `internal/media/`          | Image and video uploads, state, and transcoding                |
| `internal/runtimegateway/` | Per-user Agent container lifecycle and proxying                |
| `frontend/src/`            | Vue pages, components, state, and SDK                          |
| `frontend/e2e/`            | Chromium landscape/portrait, WebKit, and multi-user E2E tests  |
| `frontend/android/`        | Android shell, JS bridge, and release build                    |
| `docker/`                  | Marvo, Runtime, and OpenCode images                            |

## Workflow

1. Fork the repository and create a topic branch from the latest `master`.
2. Keep each change focused on one clearly defined problem; avoid unrelated formatting or refactoring.
3. Add a regression test that fails before a bug fix, or a test at the appropriate layer for new behavior.
4. Run checks that match the scope of the change.
5. Create a one-line commit using the project format.
6. Push the branch and open a pull request describing the behavior and verification.

Example:

```bash
git switch -c fix/note-conflict
# Make and verify the change
git add <files>
git commit -m "FIX: handle note revision conflicts"
git push -u origin fix/note-conflict
```

## Commit convention

Commit messages contain one line only, with no body:

```text
TYPE: concise description
```

Use these types:

| Type       | Purpose                                                                     |
| ---------- | --------------------------------------------------------------------------- |
| `ADD`      | Add a user-visible feature or capability                                    |
| `FIX`      | Correct a defect in existing behavior                                       |
| `OPT`      | Improve the experience, performance, or implementation of existing behavior |
| `REFACTOR` | Restructure code without changing expected behavior                         |
| `TEST`     | Add or adjust automated tests                                               |
| `DOCS`     | Change documentation only                                                   |
| `CHORE`    | Versioning, build, dependency, and maintenance work                         |

Keep commits focused so that the history remains easy to trace. Avoid vague subjects such as `update` or `changes`, and do not combine unrelated goals in one commit.

## Quality checks

### Go

```bash
gofmt -w <changed Go files>
go test ./...
make lint-go
```

When changing frontend embedding code guarded by the `marvo_web` build tag, also run:

```bash
make build
```

### Vue / TypeScript

```bash
npm --prefix frontend run check
npm --prefix frontend run test:e2e
```

You can pass a file or project to Playwright when running only affected E2E tests:

```bash
npm --prefix frontend run test:e2e -- e2e/core-flow.spec.ts --project=chromium-landscape
```

Responsive layout changes should cover landscape, phone portrait, touch interaction, and both color modes. To check design compatibility in WebKit, run:

```bash
make test-webkit
```

### Android

```bash
make lint-android
make test-android
```

Use `make format-android` to format Kotlin. Release builds require stable signing configuration outside the repository. Contributors must not commit `signing.properties`, JKS files, keystores, or APKs.

### Full check

Run the full project check for cross-layer or release-related changes:

```bash
make audit
```

It covers Go formatting, `go vet`, Staticcheck, frontend type checking and lint, Android static analysis, dead-code checks, unit tests, and the production frontend build.

## Product and architecture boundaries

- **One responsive frontend**: do not add `/mobile` or maintain a second mobile-specific interface.
- **User route isolation**: user pages and APIs live under `/user/{userId}`. The server must derive data directories from an authenticated user context and must never accept arbitrary filesystem paths from the browser.
- **File-first storage**: a note title is its current directory name and storage identity. Content, tags, and media remain ordinary files; the platform SQLite database stores platform control data only.
- **Conditional writes**: content and metadata updates carry a revision and instance token. Conflicts return current state so the frontend can review, merge, or retain a draft.
- **Layered authentication**: platform administration, user administration, and approved-device credentials have different purposes and cannot replace one another.
- **Per-user Agent Runtime**: an agent can work throughout its user's workspace, but cannot read another user's directory, the Runtime token, the Docker socket, or host paths.
- **Constrained Runtime gateway**: only the gateway touches the Docker API. Clients cannot choose images, mounts, networks, or container parameters.
- **Consistent UI**: reuse the existing Ark UI primitives, icon dependencies, and `frontend/src/components/x/`. Do not introduce native `alert`, `confirm`, or `prompt`, or create a duplicate component for an existing interaction.
- **Allowlisted Android bridge**: web code can use only capabilities declared in `nativeApp.ts` and validated again by Android. Never add an arbitrary native command entry point.
- **Marvo 1.0 scope**: Markdown math rendering is currently out of scope.

When changing any of these boundaries, describe the threat model, migration path, and corresponding tests in the pull request.

## Pull request requirements

A pull request should include:

- The problem or goal and why the change is needed.
- User-visible behavior and the main implementation choices.
- Checks that were run and their results.
- Screenshots or recordings for UI changes, covering affected widths and color modes.
- Effects on configuration, disk formats, migration, deployment, or Android versioning.
- Known limitations and untested real-device environments.

Keep branches reviewable. Avoid unnecessary force pushes after review begins; add follow-up commits in response to feedback.

## Reporting issues

Include:

- The Marvo version or commit hash.
- Browser, operating system, device type, screen size, and deployment method.
- Minimal reproduction steps, expected behavior, and actual behavior.
- Redacted browser console output and Marvo, Runtime, or Agent logs.
- Whether the issue is limited to one user, note, media item, provider, or the Android app.

Before posting any log or screenshot, remove passwords, cookies, tokens, API keys, TOTP secrets, private notes, and real device identifiers.
