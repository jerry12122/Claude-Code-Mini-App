## Purpose

Enforces search engine exclusion and automated scanner suppression across HTTP endpoints and HTML metadata to prevent unintended public indexing and inspection of the remote operation web interface.

## ADDED Requirements

### Requirement: Robots Exclusion Protocol
The system SHALL serve a `/robots.txt` endpoint that disallows all user agents from crawling any path on the server.

#### Scenario: Crawler requests robots.txt
- **WHEN** any user agent sends a GET request to `/robots.txt`
- **THEN** the server responds with HTTP 200, text/plain content type, and body containing `User-agent: *` followed by `Disallow: /`

### Requirement: Anti-Indexing HTTP Response Headers
The system SHALL attach crawler suppression and security headers on HTTP responses across all HTML and static endpoints.

#### Scenario: Response headers prevent search indexing
- **WHEN** a client receives any HTTP response for application pages or static resources
- **THEN** the headers contain `X-Robots-Tag: noindex, nofollow, noarchive, nosnippet`, `X-Content-Type-Options: nosniff`, and `Referrer-Policy: no-referrer`

### Requirement: HTML Robots Meta Tag
The system SHALL include a robots meta tag within the HTML head to prohibit web indexing and link following at the document level.

#### Scenario: HTML document head contains robots directive
- **WHEN** a web browser or parser inspects the root document head
- **THEN** the HTML contains `<meta name="robots" content="noindex, nofollow, noarchive">`
