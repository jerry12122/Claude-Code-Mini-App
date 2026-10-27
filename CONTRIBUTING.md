# Contributing to Claude Code Mini App

Thank you for your interest in contributing to Claude Code Mini App! This document provides guidelines for contributing to this project.

## Core Architectural Principles

Before contributing, please keep in mind the following project design choices:

1. **Single Go Binary**: The entire app (API, WebSockets, background tasks, static web assets) is distributed as a single compiled Go binary.
2. **No Frontend Build Step**: The web interface lives in `internal/static/` using React, Tailwind (CDN), and standard ES modules/Babel in-browser parsing. Do not introduce a Node.js bundling or compilation pipeline (e.g. webpack, vite) that breaks direct editing of `internal/static/`.
3. **Multi-Agent First**: Features should remain compatible across the supported coding agents (Claude Code, Cursor Agent, Codex, Kiro ACP, Antigravity) without vendor-locking.
4. **Spec-Driven Development (Spectra)**: Major features and schema updates follow SDD. Specs live in `docs/spectra/specs/`, and changes in `docs/spectra/changes/`.

## Getting Started

### Prerequisites

- [Go](https://go.dev/dl/) 1.25 or newer
- An installed and configured coding agent CLI (e.g. `claude`, `cursor-agent`, `codex`, `kiro-cli`, `agy`)
- Windows 11 (for Desktop app WebView2 / Wails development) or Linux / macOS

### Running Locally

Clone the repository:
```bash
git clone https://github.com/jerry12122/Claude-Code-Mini-App.git
cd Claude-Code-Mini-App
```

Run the server build:
```bash
go run ./cmd/server
```
Then open <http://127.0.0.1:8080> in your browser.

Run the Windows desktop build:
```bash
go run ./cmd/desktop
```

Editing frontend files in `internal/static/` is immediately visible upon refreshing the browser or pressing `F5` in the desktop window (no re-compilation needed in development mode).

### Running Tests

Run all Go unit and integration tests:
```bash
go test -v ./...
```

Run JavaScript unit tests (using Node.js standard test runner or assertion runner):
```bash
node scripts/tab-groups.test.cjs
node scripts/desktop-update.test.cjs
```

## Pull Request Guidelines

1. **Keep PRs Focused**: Each pull request should address a single bug fix, improvement, or feature.
2. **Verify Tests**: Ensure `go test ./...` passes cleanly without race conditions (`-race` where applicable).
3. **Documentation**: If your change modifies configuration options, CLI runner behavior, or user-facing features, please update the corresponding documentation in `docs/` and `README.md` (both English and Traditional Chinese `README.zh-TW.md`).
4. **Commit Messages**: Follow standard conventional commits format (e.g., `feat: ...`, `fix: ...`, `docs: ...`).

## Reporting Issues

If you encounter bugs or have feature suggestions, please check existing issues before opening a new one. Use the provided [Issue Templates](https://github.com/jerry12122/Claude-Code-Mini-App/issues/new/choose).
