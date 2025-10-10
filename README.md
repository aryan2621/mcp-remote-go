# mcp-remote-go

A simple Go bridge that connects stdio MCP clients to remote HTTP/SSE servers with header-based authentication.

## Installation
```bash
go install github.com/rishabh-verma/mcp-remote-go@latest
```

## Quick Startl
```bash
# Basic connection
mcp-remote-go --url=https://mcp.example.com

# With API key
mcp-remote-go --url=https://api.example.com --header "X-API-Key: your-key"

# SSE endpoint
mcp-remote-go --url=https://mcp.example.com/sse --header "Authorization: Bearer token"
```

## Features
✅ Simple header-based authentication  
✅ Auto transport detection (HTTP/SSE)  
✅ MCP Protocol 2025-06-18 compliant  
✅ No OAuth complexity - just headers  
✅ Works with any MCP server  

## Configuration

### Claude Desktop
**macOS:** `~/Library/Application Support/Claude/claude_desktop_config.json`

**Basic connection:**
```json
{
  "mcpServers": {
    "remote": {
      "command": "mcp-remote-go",
      "args": ["--url=https://mcp.example.com"]
    }
  }
}
```

**With API key:**
```json
{
  "mcpServers": {
    "remote": {
      "command": "mcp-remote-go",
      "args": [
        "--url=https://api.example.com",
        "--header", "X-API-Key: your-api-key"
      ]
    }
  }
}
```

**SSE endpoint:**
```json
{
  "mcpServers": {
    "remote": {
      "command": "mcp-remote-go",
      "args": [
        "--url=https://mcp.example.com/sse",
        "--header", "Authorization: Bearer your-token"
      ]
    }
  }
}
```

## Flags
```bash
mcp-remote-go --url=<server-url> [options]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--url` | - | Server URL (required) |
| `--header` | - | Custom header (repeatable) |
| `--transport` | http-first | http-first, sse-first, http-only, sse-only |
| `--protocol-version` | 2025-06-18 | MCP protocol version |
| `--allow-http` | false | Allow HTTP (dev only) |
| `--debug` | false | Enable debug logging |

## Authentication

### No Authentication
Connect without any authentication:

```bash
mcp-remote-go --url=https://mcp.example.com
```

### Header-based Authentication
Use custom headers for API keys, tokens, etc.:

```bash
# API Key
mcp-remote-go --url=https://api.example.com --header "X-API-Key: abc123"

# Bearer Token
mcp-remote-go --url=https://api.example.com --header "Authorization: Bearer token"

# Multiple headers
mcp-remote-go --url=https://api.example.com \
  --header "Authorization: Bearer token" \
  --header "X-Tenant: acme"
```

## Transport Detection

| Mode | Behavior |
|------|----------|
| `http-first` | Try HTTP → fallback SSE (default) |
| `sse-first` | Try SSE → fallback HTTP |
| `http-only` | HTTP only |
| `sse-only` | SSE only |

## Examples

```bash
# Basic connection
mcp-remote-go --url=https://mcp.example.com

# With API key
mcp-remote-go --url=https://api.example.com --header "X-API-Key: your-key"

# SSE endpoint
mcp-remote-go --url=https://mcp.example.com/sse --header "Authorization: Bearer token"

# Multiple headers
mcp-remote-go --url=https://api.example.com \
  --header "Authorization: Bearer token" \
  --header "X-Tenant: acme"

# Debug mode
mcp-remote-go --url=https://mcp.example.com --debug

# Development (HTTP)
mcp-remote-go --url=http://localhost:8080 --allow-http --debug
```

## Troubleshooting

**Debug logs:**
```bash
mcp-remote-go --url=https://mcp.example.com --debug
```

**Try different transport:**
```bash
mcp-remote-go --url=https://mcp.example.com --transport sse-only
```

**Check authentication:**
```bash
# Test with debug to see what's happening
mcp-remote-go --url=https://api.example.com --header "X-API-Key: abc" --debug
```

## Security
🔒 HTTPS enforced (dev: `--allow-http`)  
🔑 Simple header-based authentication  
🛡️ No complex OAuth flows to manage  

## Spec Compliance
- MCP Protocol 2025-06-18
- Simple HTTP/SSE transport

## Development
```bash
git clone https://github.com/rishabh-verma/mcp-remote-go
cd mcp-remote-go
go mod download
make build
./bin/mcp-remote-go --url=https://mcp.example.com --debug
```

## License
MIT

## Links
- [MCP Specification](https://modelcontextprotocol.io/)
- [Original TypeScript](https://github.com/modelcontextprotocol/remote)