# web-pwa-support Specification

## Purpose

Provides Progressive Web App (PWA) manifest and installation capabilities, Apple web app configuration, service worker caching for static application assets with strict API bypass, and mobile viewport interactive-widget keyboard resizing support for standalone web operation.

## Requirements

### Requirement: PWA Manifest Definition
The system SHALL serve a valid Web App Manifest at `/manifest.webmanifest` defining application name, start URL, standalone display mode, background color, theme color, and icons.

#### Scenario: Manifest endpoint requested
- **WHEN** a client sends a GET request to `/manifest.webmanifest`
- **THEN** the server returns HTTP 200 with content type `application/manifest+json` containing `display: "standalone"`, `start_url: "/"`, and application metadata


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
### Requirement: Mobile Standalone and Viewport Metadata
The system SHALL provide HTML head metadata specifying Web App Manifest linkage, Apple mobile web app capability tags, theme color, and viewport interactive-widget keyboard resize support.

#### Scenario: Index HTML renders mobile web and PWA metadata
- **WHEN** a client loads the root index HTML
- **THEN** the document contains `<link rel="manifest" href="/manifest.webmanifest">`, Apple standalone meta tags, and `<meta name="viewport">` with `interactive-widget=resizes-content`


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
### Requirement: Service Worker Static Asset Caching with API Bypass
The system SHALL register and serve a Service Worker at `/sw.js` that caches static frontend shell assets while strictly bypassing all `/api/*` network requests and real-time streaming connections.

#### Scenario: Service Worker bypasses API requests
- **WHEN** a network request begins with `/api/` or involves WebSocket or Server-Sent Events
- **THEN** the Service Worker passes the request directly to the network without caching or interception

#### Scenario: Service Worker serves cached static assets
- **WHEN** the browser requests a previously cached static script, stylesheet, or icon
- **THEN** the Service Worker serves the asset from cache if network is unavailable or employs a cache-first or network-first strategy for static assets

##### Example: Static asset cache response
- **GIVEN** `/manifest.webmanifest` and `/favicon.ico` are precached in the service worker cache
- **WHEN** the client requests `/favicon.ico` while disconnected
- **THEN** the Service Worker returns the cached `/favicon.ico` response with HTTP status 200

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