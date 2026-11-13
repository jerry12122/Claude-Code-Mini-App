# crawler-stealth-guard Specification

## Purpose

Enforces search engine exclusion and automated scanner suppression across HTTP endpoints and HTML metadata to prevent unintended public indexing and inspection of the remote operation web interface.

## Requirements

### Requirement: Robots Exclusion Protocol
The system SHALL serve a `/robots.txt` endpoint that disallows all user agents from crawling any path on the server.

#### Scenario: Crawler requests robots.txt
- **WHEN** any user agent sends a GET request to `/robots.txt`
- **THEN** the server responds with HTTP 200, text/plain content type, and body containing `User-agent: *` followed by `Disallow: /`


<!-- @trace
source: web-pwa-and-crawler-guard
updated: 2026-11-13
code:
  - internal/static/assets.go
  - internal/static/manifest.webmanifest
  - internal/static/index.html
  - internal/static/js/hooks/useNewSessionForm.js
  - scripts/service-worker.test.cjs
  - scripts/context-menu.test.cjs
  - internal/mcp/tools.go
  - internal/static/sw.js
  - internal/api/upload.go
  - internal/api/session.go
  - internal/agent/factory.go
  - internal/static/js/chat/ChatView.js
  - internal/static/robots.txt
  - internal/ws/handler.go
  - scripts/mobile-composer-mock.test.py
  - docs/prototypes/mobile-composer-sheet.dc.html
  - internal/static/js/chat/ForwardModal.js
  - README.md
  - internal/server/server.go
  - CHANGELOG.md
  - README.zh-TW.md
  - internal/static/assets_production.go
  - internal/db/busy.go
  - internal/static/js/ui/SessionTabs.js
tests:
  - internal/server/start_helper_test.go
  - internal/server/crawler_guard_test.go
  - internal/mcp/kiro_readonly_test.go
  - internal/db/busy_test.go
  - internal/agent/factory_test.go
  - docs/spec/kiro-cli.md
  - internal/ws/kiro_readonly_test.go
-->

---
### Requirement: Anti-Indexing HTTP Response Headers
The system SHALL attach crawler suppression and security headers on HTTP responses across all HTML and static endpoints.

#### Scenario: Response headers prevent search indexing
- **WHEN** a client receives any HTTP response for application pages or static resources
- **THEN** the headers contain `X-Robots-Tag: noindex, nofollow, noarchive, nosnippet`, `X-Content-Type-Options: nosniff`, and `Referrer-Policy: no-referrer`


<!-- @trace
source: web-pwa-and-crawler-guard
updated: 2026-11-13
code:
  - internal/static/assets.go
  - internal/static/manifest.webmanifest
  - internal/static/index.html
  - internal/static/js/hooks/useNewSessionForm.js
  - scripts/service-worker.test.cjs
  - scripts/context-menu.test.cjs
  - internal/mcp/tools.go
  - internal/static/sw.js
  - internal/api/upload.go
  - internal/api/session.go
  - internal/agent/factory.go
  - internal/static/js/chat/ChatView.js
  - internal/static/robots.txt
  - internal/ws/handler.go
  - scripts/mobile-composer-mock.test.py
  - docs/prototypes/mobile-composer-sheet.dc.html
  - internal/static/js/chat/ForwardModal.js
  - README.md
  - internal/server/server.go
  - CHANGELOG.md
  - README.zh-TW.md
  - internal/static/assets_production.go
  - internal/db/busy.go
  - internal/static/js/ui/SessionTabs.js
tests:
  - internal/server/start_helper_test.go
  - internal/server/crawler_guard_test.go
  - internal/mcp/kiro_readonly_test.go
  - internal/db/busy_test.go
  - internal/agent/factory_test.go
  - docs/spec/kiro-cli.md
  - internal/ws/kiro_readonly_test.go
-->

---
### Requirement: HTML Robots Meta Tag
The system SHALL include a robots meta tag within the HTML head to prohibit web indexing and link following at the document level.

#### Scenario: HTML document head contains robots directive
- **WHEN** a web browser or parser inspects the root document head
- **THEN** the HTML contains `<meta name="robots" content="noindex, nofollow, noarchive">`

<!-- @trace
source: web-pwa-and-crawler-guard
updated: 2026-11-13
code:
  - internal/static/assets.go
  - internal/static/manifest.webmanifest
  - internal/static/index.html
  - internal/static/js/hooks/useNewSessionForm.js
  - scripts/service-worker.test.cjs
  - scripts/context-menu.test.cjs
  - internal/mcp/tools.go
  - internal/static/sw.js
  - internal/api/upload.go
  - internal/api/session.go
  - internal/agent/factory.go
  - internal/static/js/chat/ChatView.js
  - internal/static/robots.txt
  - internal/ws/handler.go
  - scripts/mobile-composer-mock.test.py
  - docs/prototypes/mobile-composer-sheet.dc.html
  - internal/static/js/chat/ForwardModal.js
  - README.md
  - internal/server/server.go
  - CHANGELOG.md
  - README.zh-TW.md
  - internal/static/assets_production.go
  - internal/db/busy.go
  - internal/static/js/ui/SessionTabs.js
tests:
  - internal/server/start_helper_test.go
  - internal/server/crawler_guard_test.go
  - internal/mcp/kiro_readonly_test.go
  - internal/db/busy_test.go
  - internal/agent/factory_test.go
  - docs/spec/kiro-cli.md
  - internal/ws/kiro_readonly_test.go
-->