# Security Policy

## Supported Versions

Only the latest minor release receives security updates.

| Version | Supported |
| --- | --- |
| 0.15.x | :white_check_mark: |
| < 0.15 | :x: |

## Scope & Security Boundary

Claude Code Mini App is designed as a self-hosted console for supervising local AI coding agents. Given that the app can run shell commands, manage local sessions, and expose an MCP server, please keep the following security properties in mind:

- **Authentication**: `web.password` or Telegram `initData` protect access. `no_auth: true` is strictly for isolated local environments and must never be exposed publicly.
- **Direct Shell (`shell.enabled`)**: Directly executes commands on the host in the session's `work_dir`. It is disabled by default.
- **Temporary Sharing**: Grants guests access via a PIN. `viewer` role is read-only; `editor` grants full interaction.
- **MCP Token**: Grants access to session control via `/mcp`. Keep `mcp_token` secret.

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly. **Do not disclose security vulnerabilities in public GitHub issues or discussions.**

Please report vulnerabilities through one of the following channels:

1. **GitHub Security Advisories (Preferred)**:
   Submit a private advisory via [GitHub Security Advisory](https://github.com/jerry12122/Claude-Code-Mini-App/security/advisories/new).
2. **Email**:
   Contact Jerry Liu directly at [jerry.fxgames@gmail.com](mailto:jerry.fxgames@gmail.com).

### Information to Include

Please provide:
- A description of the vulnerability and its potential impact.
- Step-by-step reproduction instructions or a Proof of Concept (PoC).
- Relevant configuration settings (`config.yaml`), if applicable.
- Operating system and app version.

You will receive an initial response acknowledging receipt within 48 hours. If the report is confirmed, a patch will be released promptly along with a release note crediting the reporter (unless you prefer anonymity).
