# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/). Release notes on GitHub are generated from this file.

## [Unreleased]

### Changed
- Release notes are now taken from this changelog.

## [0.1.3] - 2026-11-23

### Changed
- When Spoolman answers with a redirect (HTTP 3xx), the server now returns a clear error:
  *"redirect not followed (credentials are only sent to the configured URL); point SPOOLMAN_URL at the
  final Spoolman address"* instead of the raw redirect page. Redirects are still never followed.

### Tests
- The redirect regression test now covers 301/302/303/307/308, GET and DELETE, and bearer token,
  custom headers and Basic auth. It checks that the redirect target is never contacted.

**Upgrade notes:** none. If you see the new error, set `SPOOLMAN_URL` to the address Spoolman really
answers on (e.g. `https://` instead of `http://`, or without a path prefix).

## [0.1.2] - 2026-11-22

Security release. All findings of a full source code security audit of v0.1.1 are fixed
(none rated higher than *low*). Updating is recommended.

### Security
- **`unlink_tag` could delete a whole spool:** a tag UID of `../` made Spoolman resolve the request to
  `DELETE /spool/{id}`. Path arguments (tag UIDs, setting keys, locations, field names, …) that are empty,
  `.`, `..` or contain `/` or `\` are now rejected for every tool.
- **Credentials could leak via redirects:** the Spoolman client followed redirects, so custom headers
  (`SPOOLMAN_HEADERS`) could reach other hosts and a bearer token could be sent over plain HTTP after an
  https→http redirect. Redirects are no longer followed.
- **`set_setting` could wipe extra field definitions** (`extra_fields_*`) even with `ALLOW_DELETE=false`.
  These keys are now refused (use `set_extra_field` / `delete_extra_field`), and resetting a setting to
  its default requires `confirm=true`.
- **Typos in safety switches were ignored:** e.g. `READ_ONLY=yes` silently left write tools enabled.
  Invalid boolean values now stop the server at startup; `yes/no` and `on/off` are accepted.
- **Passwords in `SPOOLMAN_URL` were logged:** credentials in the URL (`user:pass@`) are now moved to
  Basic auth and never logged; configuration errors never echo secrets.
- **Tool annotations:** tools that overwrite, merge or reset data (`update_*`, `rename_*`, `set_setting`,
  `set_extra_field`, `use_spool`, `measure_spool`) are now marked `destructiveHint: true`, so MCP clients
  can ask before running them.

### Added
- `MAX_RESULT_BYTES` (default 4 MiB): very large tool results are truncated with a hint.
- `MCP_HTTP_MAX_CONCURRENT` (default 64): limit for concurrent MCP HTTP requests/SSE streams.
- Signed build provenance for release binaries and container images
  (`gh attestation verify …`, see README).
- Dependabot for Go modules, GitHub Actions and the Docker base image.

### Changed
- Streamable HTTP sessions are bound to the token that created them.
- Oversized Spoolman responses are reported as an error instead of being cut off silently.
- `SPOOLMAN_URL` with a query or fragment is rejected; an `Authorization` header in `SPOOLMAN_HEADERS`
  together with bearer/Basic auth is rejected.
- CI: actions pinned by commit SHA, base images by digest; pull requests build without write
  permissions; `latest` is only set for non-prerelease tags.
- Docs: token via file/Docker secret, example compose publishes on `127.0.0.1` only.

**Upgrade notes:**
- An invalid value in `READ_ONLY`, `ALLOW_DELETE` or other boolean variables now prevents startup.
- `set_setting` without `value` needs `confirm=true`; `extra_fields_*` keys are refused.
- `ALLOW_DELETE` still defaults to `true`.

## [0.1.1] - 2026-11-22

### Changed
- Requires Go 1.27.4; dependencies updated.
- CI takes the Go version from `go.mod`.
- GitHub Actions updated to the current major versions (Node 24).

**Upgrade notes:** none, no functional changes.

## [0.1.0] - 2026-11-21

First release.

### Added
- MCP server for [Spoolman](https://github.com/Donkie/Spoolman) as a single static Go binary,
  built on the official MCP Go SDK.
- Transports: `stdio`, Streamable HTTP (`/mcp`) and legacy HTTP+SSE (`/sse`).
- Bearer token auth for HTTP (multiple tokens, constant-time compare, refuses to start unauthenticated
  unless `MCP_ALLOW_NO_AUTH=true`); `/healthz` and `/readyz` endpoints.
- 38 tools: vendors, filaments, spools (create/update/use/measure/delete), locations, NFC tags, extra
  fields, settings, export, backups and import from the SpoolmanDB catalogue.
- Safety switches: `READ_ONLY`, `ALLOW_DELETE`, and `confirm=true` for every delete.
- Upstream auth towards Spoolman: bearer, Basic or custom headers (e.g. Cloudflare Access).
- Portable tool schemas (no `$ref`, no union types) for strict clients such as Gemini and OpenAI.
- Container image `ghcr.io/virtualben89/spoolman-mcp-go` (`FROM scratch`, non-root, ~14 MB,
  amd64/arm64/armv7) and release binaries for Linux, macOS and Windows.

[Unreleased]: https://github.com/virtualben89/spoolman-mcp-go/compare/v0.1.3...HEAD
[0.1.3]: https://github.com/virtualben89/spoolman-mcp-go/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/virtualben89/spoolman-mcp-go/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/virtualben89/spoolman-mcp-go/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/virtualben89/spoolman-mcp-go/commits/v0.1.0
