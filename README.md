# spoolman-mcp-go

A fast, dependency-free [Model Context Protocol](https://modelcontextprotocol.io) server for
[Spoolman](https://github.com/Donkie/Spoolman), the self-hosted 3D printing filament inventory.

It lets any MCP-capable AI agent (Claude Desktop/Code, ChatGPT, Cursor, VS Code Copilot, Gemini CLI,
Codex, n8n, Hermes, LibreChat, Open WebUI, …) **read and manage** your vendors, filaments and spools.

- **Single static Go binary**, built on the official [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)
- **Transports:** `stdio`, **Streamable HTTP** (`/mcp`) and legacy **HTTP+SSE** (`/sse`)
- **Bearer token auth** for HTTP (multiple tokens, constant-time compare, refuses to start unauthenticated)
- **Read & write:** 38 tools covering the full Spoolman API, including NFC tags, extra fields, settings,
  backups and import from the SpoolmanDB catalogue
- **Safety switches:** `READ_ONLY=true` hides all write tools, `ALLOW_DELETE=false` hides delete tools,
  and every delete requires `confirm=true`
- **Upstream auth** towards Spoolman (Bearer, Basic or custom headers, e.g. Cloudflare Access) for
  instances behind a reverse proxy
- **Tiny image:** `FROM scratch`, non-root, ~14 MB, `linux/amd64`, `linux/arm64`, `linux/arm/v7`
- Portable tool schemas (no `$ref`, no union types) so strict clients such as Gemini and OpenAI accept them

## Quick start

### Docker (HTTP)

```bash
docker run -d --name spoolman-mcp -p 8080:8080 \
  -e SPOOLMAN_URL=http://spoolman:7912 \
  -e MCP_AUTH_TOKEN="$(openssl rand -hex 32)" \
  ghcr.io/virtualben89/spoolman-mcp-go:latest
```

The MCP endpoint is `http://<host>:8080/mcp` (legacy SSE: `/sse`). Clients must send
`Authorization: Bearer <token>`. See [`examples/docker-compose.yml`](examples/docker-compose.yml).

### Docker (stdio)

```bash
docker run -i --rm -e MCP_TRANSPORT=stdio -e SPOOLMAN_URL=http://spoolman:7912 \
  ghcr.io/virtualben89/spoolman-mcp-go:latest
```

### Binary

Download a binary from [Releases](https://github.com/virtualben89/spoolman-mcp-go/releases) or
`go install github.com/virtualben89/spoolman-mcp-go/cmd/spoolman-mcp@latest`, then:

```bash
SPOOLMAN_URL=http://spoolman:7912 spoolman-mcp            # stdio (default)
SPOOLMAN_URL=http://spoolman:7912 MCP_TRANSPORT=http MCP_AUTH_TOKEN=... spoolman-mcp
```

## Configuration

All options can be set as environment variables (or flags, see `spoolman-mcp -h`).
Secrets are read from env only; every secret also supports a `*_FILE` variant (Docker secrets).

| Variable | Default | Description |
|---|---|---|
| `SPOOLMAN_URL` | – (required) | Spoolman base URL, e.g. `http://spoolman:7912` (`/api/v1` is optional) |
| `SPOOLMAN_TIMEOUT` | `30s` | Timeout for Spoolman requests |
| `SPOOLMAN_BEARER_TOKEN` | – | Send `Authorization: Bearer …` to Spoolman |
| `SPOOLMAN_BASIC_USER` / `SPOOLMAN_BASIC_PASSWORD` | – | Send HTTP Basic auth to Spoolman |
| `SPOOLMAN_HEADERS` | – | Extra headers, `Name: value; Other: value` (e.g. Cloudflare Access service tokens) |
| `SPOOLMAN_CA_FILE` | – | Extra CA bundle (PEM) for a private CA |
| `SPOOLMAN_INSECURE_SKIP_VERIFY` | `false` | Skip TLS verification (not recommended) |
| `MCP_TRANSPORT` | `stdio` (`http` in Docker) | `stdio` or `http` |
| `MCP_HTTP_ADDR` | `:8080` | Listen address |
| `MCP_HTTP_PATH` | `/mcp` | Streamable HTTP endpoint |
| `MCP_AUTH_TOKEN` | – | Bearer token(s) for HTTP clients, comma-separated, min. 16 chars. **Required** for HTTP |
| `MCP_ALLOW_NO_AUTH` | `false` | Explicitly allow HTTP without a token (only behind an authenticating proxy) |
| `MCP_HTTP_ENABLE_SSE` | `true` | Also serve the legacy HTTP+SSE transport on `/sse` |
| `MCP_HTTP_STATELESS` | `false` | Stateless Streamable HTTP (no sessions; good for load balancers/serverless) |
| `MCP_HTTP_JSON_RESPONSE` | `false` | Reply with `application/json` instead of SSE streams |
| `MCP_HTTP_SESSION_TIMEOUT` | `30m` | Close idle sessions after this duration (`0` = never) |
| `READ_ONLY` | `false` | Only expose read tools |
| `ALLOW_DELETE` | `true` | Expose delete tools (they still require `confirm=true`) |
| `TOOL_PREFIX` | – | Prefix for tool names, e.g. `spoolman_` for clients without namespacing |
| `DEFAULT_LIMIT` | `50` | Default page size of list tools (`0` = unlimited) |
| `LOG_LEVEL` / `LOG_FORMAT` | `info` / `text` | `debug…error` / `text` or `json` (always logged to stderr) |

HTTP endpoints: `/mcp` (+ `/sse`) require the bearer token; `/healthz` (liveness) and `/readyz`
(checks Spoolman) are public. The image contains a `healthcheck` subcommand used by Docker's `HEALTHCHECK`.

> **Security note:** the token protects the MCP endpoint, but it travels in plain text over HTTP.
> Put the server behind TLS (e.g. Traefik/Caddy) or keep it on a trusted network/VPN.
> stdio has no network exposure and therefore no token.

## Client configuration

<details><summary><b>Claude Code</b></summary>

```bash
# HTTP
claude mcp add --transport http spoolman https://spoolman-mcp.example.com/mcp \
  --header "Authorization: Bearer $TOKEN"
# stdio
claude mcp add spoolman -e SPOOLMAN_URL=http://spoolman:7912 -- spoolman-mcp
```
</details>

<details><summary><b>Claude Desktop / Cursor / Windsurf / generic <code>mcpServers</code> JSON</b></summary>

```json
{
  "mcpServers": {
    "spoolman": {
      "command": "spoolman-mcp",
      "env": { "SPOOLMAN_URL": "http://spoolman:7912" }
    }
  }
}
```

Via Docker instead of a local binary:

```json
{
  "mcpServers": {
    "spoolman": {
      "command": "docker",
      "args": ["run", "-i", "--rm", "-e", "MCP_TRANSPORT=stdio", "-e", "SPOOLMAN_URL",
               "ghcr.io/virtualben89/spoolman-mcp-go:latest"],
      "env": { "SPOOLMAN_URL": "http://spoolman:7912" }
    }
  }
}
```

Remote HTTP (Cursor and others that support `url` + `headers`):

```json
{
  "mcpServers": {
    "spoolman": {
      "url": "https://spoolman-mcp.example.com/mcp",
      "headers": { "Authorization": "Bearer <token>" }
    }
  }
}
```
</details>

<details><summary><b>VS Code (GitHub Copilot)</b> – <code>.vscode/mcp.json</code></summary>

```json
{
  "inputs": [{ "type": "promptString", "id": "spoolman-token", "description": "Spoolman MCP token", "password": true }],
  "servers": {
    "spoolman": {
      "type": "http",
      "url": "https://spoolman-mcp.example.com/mcp",
      "headers": { "Authorization": "Bearer ${input:spoolman-token}" }
    }
  }
}
```
</details>

<details><summary><b>Gemini CLI</b> – <code>~/.gemini/settings.json</code></summary>

```json
{
  "mcpServers": {
    "spoolman": {
      "httpUrl": "https://spoolman-mcp.example.com/mcp",
      "headers": { "Authorization": "Bearer <token>" }
    }
  }
}
```
</details>

<details><summary><b>OpenAI Codex CLI</b> – <code>~/.codex/config.toml</code></summary>

```toml
[mcp_servers.spoolman]
url = "https://spoolman-mcp.example.com/mcp"
bearer_token_env_var = "SPOOLMAN_MCP_TOKEN"
```
</details>

<details><summary><b>Hermes Agent</b> – <code>config.yaml</code></summary>

```yaml
mcp_servers:
  spoolman:
    url: https://spoolman-mcp.example.com/mcp
    headers:
      Authorization: "Bearer ${SPOOLMAN_MCP_TOKEN}"
```
</details>

<details><summary><b>n8n, LibreChat, Open WebUI, OpenAI Agents SDK, …</b></summary>

Use the Streamable HTTP URL `https://<host>/mcp` with header `Authorization: Bearer <token>`.
Older clients that only speak the 2024 HTTP+SSE transport can use `https://<host>/sse`.
</details>

## Tools

| Read | Write | Delete (`confirm=true`) |
|---|---|---|
| `get_info` – version & health | `create_vendor`, `update_vendor` | `delete_spool` |
| `search` – across spools, filaments, vendors | `create_filament`, `update_filament` | `delete_filament` |
| `list_spools`, `get_spool` | `import_external_filament` – from SpoolmanDB | `delete_vendor` |
| `list_spool_groups` – inventory per filament/vendor/material/location | `create_spool`, `update_spool` | `delete_extra_field` |
| `list_filaments`, `get_filament` | `use_spool` – book consumption (g or mm) | `unlink_tag` |
| `list_vendors`, `get_vendor` | `measure_spool` – update from scale weight | |
| `list_distinct_values` – materials, locations, article/lot numbers | `rename_location`, `rename_spool_field_value` | |
| `list_extra_fields`, `list_extra_field_values` | `set_extra_field` | |
| `get_settings` | `set_setting`, `create_backup` | |
| `search_external_filaments`, `list_external_materials` | `link_tag`, `report_tag_scan` | |
| `export` – CSV/JSON | | |
| `list_tag_readers` | | |

Notes for agents:
- Update tools are partial: only given fields change. Use `clear_fields` to set fields to empty.
- `extra` values are passed as native JSON values and encoded for Spoolman automatically.
- List tools return `{total, count, offset, limit, has_more, items}`.
- All tools carry MCP annotations (`readOnlyHint`, `destructiveHint`, `idempotentHint`) so clients
  can auto-approve reads and ask before writes.

## Development

```bash
go test -race ./...
# integration tests against a DISPOSABLE Spoolman (creates and deletes data!)
docker run -d -p 7912:8000 ghcr.io/donkie/spoolman:latest
SPOOLMAN_TEST_URL=http://localhost:7912 go test -tags integration ./internal/tools/
```

CI runs unit tests, the integration suite against a real Spoolman service container, an image smoke
test, and publishes multi-arch images to GHCR (`edge` from `main`, semver + `latest` from tags).

## License

MIT
