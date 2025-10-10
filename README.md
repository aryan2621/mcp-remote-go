# mcp-remote-go

A simple Go bridge that connects stdio MCP clients to remote HTTP/SSE servers with header-based authentication.

## Installation
```bash
go install github.com/rishabh-verma/mcp-remote-go@latest
```

## Quick Start
```bash
# Basic connection
mcp-remote-go --url=https://mcp.example.com

# With API key
mcp-remote-go --url=https://api.example.com --header "X-API-Key: your-key"

# SSE endpoint
mcp-remote-go --url=https://mcp.example.com/sse --header "Authorization: Bearer token"
```

## Claude Desktop Configuration
**macOS:** `~/Library/Application Support/Claude/claude_desktop_config.json`

```json
{
  "mcpServers": {
    "remote": {
      "command": "mcp-remote-go",
      "args": [
        "--url=https://mcp.example.com",
        "--header", "X-API-Key: your-api-key"
      ]
    }
  }
}
```

## Troubleshooting

**Binary not found:**
```bash
# Find the binary location
which mcp-remote-go

# Use full path in Claude Desktop config
{
  "mcpServers": {
    "remote": {
      "command": "/full/path/to/mcp-remote-go",
      "args": ["--url=https://mcp.example.com"]
    }
  }
}
```

**Debug mode:**
```bash
mcp-remote-go --url=https://mcp.example.com --debug
```

## Flags
| Flag | Default | Description |
|------|---------|-------------|
| `--url` | - | Server URL (required) |
| `--header` | - | Custom header (repeatable) |
| `--transport` | http-first | http-first, sse-first, http-only, sse-only |
| `--debug` | false | Enable debug logging |

## License
MIT