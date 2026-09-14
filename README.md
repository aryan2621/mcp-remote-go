# mcp-remote-go

A simple Go bridge that connects stdio MCP clients to remote HTTP/SSE servers. Header auth and MCP OAuth (PKCE, OS keychain).

## Installation
```bash
go install github.com/aryan2621/mcp-remote-go@latest
```

The binary is written to `$(go env GOPATH)/bin`.

Pre-built files are GitHub Release assets (`mcp-remote-go-<os>-<arch>`). They exist only after the release workflow on `main` publishes the `latest` tag.

## Quick Start
```bash
# Basic connection
mcp-remote-go --url=https://mcp.example.com

# With API key (skips OAuth)
mcp-remote-go --url=https://api.example.com --header "X-API-Key: your-key"

# OAuth is on by default. First 401 opens a browser; tokens go in the OS keychain.
mcp-remote-go --url=https://mcp.example.com
```

## MCP clients

Use an absolute path for `command`. GUI apps often cannot see your shell PATH.

**Claude Desktop** (`~/Library/Application Support/Claude/claude_desktop_config.json` or `%APPDATA%\Claude\claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "remote": {
      "command": "/absolute/path/to/mcp-remote-go",
      "args": [
        "--url=https://mcp.example.com",
        "--header",
        "X-API-Key: your-api-key"
      ]
    }
  }
}
```

**Cursor** (`.cursor/mcp.json` or `~/.cursor/mcp.json`) uses the same `mcpServers` shape.

**VS Code** (`.vscode/mcp.json` or MCP: Open User Configuration):

```json
{
  "servers": {
    "remote": {
      "type": "stdio",
      "command": "/absolute/path/to/mcp-remote-go",
      "args": [
        "--url=https://mcp.example.com",
        "--header",
        "X-API-Key: your-api-key"
      ]
    }
  }
}
```

**Claude Code** (`.mcp.json`) uses `mcpServers` with `"type": "stdio"`. **Windsurf** uses `mcpServers` in `~/.codeium/windsurf/mcp_config.json`. **Cline** uses `mcpServers` in `~/.cline/mcp.json`. **Zed** uses `context_servers` in settings.json.

## Troubleshooting

**Binary not found:**
```bash
# Find the binary location
which mcp-remote-go

# Use that full path in the client config
```

**Debug mode:**
```bash
mcp-remote-go --url=https://mcp.example.com --debug
```

## Flags
| Flag | Default | Description |
|------|---------|-------------|
| `--url` | - | Server URL (required unless set in `--config`) |
| `--config` | - | JSON config file (flags override file values) |
| `--header` | - | Custom header (repeatable; env vars expanded) |
| `--transport` | http-first | http-first, sse-first, http-only, sse-only |
| `--timeout` | 60 | HTTP request timeout in seconds (not SSE streams) |
| `--protocol-version` | 2025-06-18 | MCP protocol version |
| `--allow-http` | false | Allow HTTP connections |
| `--no-oauth` | false | Disable OAuth; use `--header` only |
| `--client-id` | - | OAuth client id if dynamic registration is unavailable |
| `--client-secret` | - | OAuth client secret (confidential clients) |
| `--logout` | false | Delete stored OAuth tokens for `--url` and exit |
| `--debug` | false | Enable debug logging |

The GitHub repository is https://github.com/aryan2621/mcp-remote-go.
