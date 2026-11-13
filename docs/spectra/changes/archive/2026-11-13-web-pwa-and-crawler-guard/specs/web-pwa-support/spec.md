## Purpose

Provides Progressive Web App (PWA) manifest and installation capabilities, Apple web app configuration, service worker caching for static application assets with strict API bypass, and mobile viewport interactive-widget keyboard resizing support for standalone web operation.

## ADDED Requirements

### Requirement: PWA Manifest Definition
The system SHALL serve a valid Web App Manifest at `/manifest.webmanifest` defining application name, start URL, standalone display mode, background color, theme color, and icons.

#### Scenario: Manifest endpoint requested
- **WHEN** a client sends a GET request to `/manifest.webmanifest`
- **THEN** the server returns HTTP 200 with content type `application/manifest+json` containing `display: "standalone"`, `start_url: "/"`, and application metadata

### Requirement: Mobile Standalone and Viewport Metadata
The system SHALL provide HTML head metadata specifying Web App Manifest linkage, Apple mobile web app capability tags, theme color, and viewport interactive-widget keyboard resize support.

#### Scenario: Index HTML renders mobile web and PWA metadata
- **WHEN** a client loads the root index HTML
- **THEN** the document contains `<link rel="manifest" href="/manifest.webmanifest">`, Apple standalone meta tags, and `<meta name="viewport">` with `interactive-widget=resizes-content`

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
