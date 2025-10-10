package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/rishabh-verma/mcp-remote-go/pkg/bridge"
	"github.com/rishabh-verma/mcp-remote-go/pkg/config"
)

var (
	serverURL       = flag.String("url", "", "Remote MCP server URL (required, e.g., --url=https://mcp.example.com)")
	allowHTTP       = flag.Bool("allow-http", false, "Allow HTTP connections")
	debug           = flag.Bool("debug", false, "Enable debug logging")
	transport       = flag.String("transport", "http-first", "Transport strategy: http-first, sse-first, http-only, sse-only")
	protocolVersion = flag.String("protocol-version", "2025-06-18", "MCP protocol version")
	headers         headerFlags
)

type headerFlags []string

func (h *headerFlags) String() string {
	return strings.Join(*h, ", ")
}

func (h *headerFlags) Set(value string) error {
	*h = append(*h, value)
	return nil
}

func init() {
	flag.Var(&headers, "header", "Custom headers (can be repeated, e.g., --header='X-API-Key: value')")
}

func main() {
	flag.Parse()

	// Validate required parameters
	if *serverURL == "" {
		printHelp()
		os.Exit(1)
	}

	// Validate URL format
	if !strings.HasPrefix(*serverURL, "http://") && !strings.HasPrefix(*serverURL, "https://") {
		fmt.Fprintf(os.Stderr, "Error: URL must start with http:// or https://\n")
		fmt.Fprintf(os.Stderr, "Got: %s\n", *serverURL)
		os.Exit(1)
	}

	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".mcp-auth")
	if dir := os.Getenv("MCP_REMOTE_CONFIG_DIR"); dir != "" {
		configDir = dir
	}

	cfg := &config.Config{
		ServerURL:       *serverURL,
		AllowHTTP:       *allowHTTP,
		Debug:           *debug,
		Transport:       *transport,
		Headers:         parseHeaders(headers),
		ConfigDir:       configDir,
		ProtocolVersion: *protocolVersion,
	}

	if cfg.Debug {
		log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)
	}

	if err := os.MkdirAll(cfg.ConfigDir, 0700); err != nil {
		log.Fatalf("Failed to create config directory: %v", err)
	}

	ctx := context.Background()
	b := bridge.NewBridge(cfg)

	if err := b.Run(ctx); err != nil {
		log.Fatalf("Bridge failed: %v", err)
	}
}

func parseHeaders(headers []string) map[string]string {
	result := make(map[string]string)
	for _, h := range headers {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			value = os.ExpandEnv(value)
			result[key] = value
		}
	}
	return result
}

func printHelp() {
	fmt.Fprintf(os.Stderr, "\n")
	fmt.Fprintf(os.Stderr, "🚀 MCP Remote Go\n")
	fmt.Fprintf(os.Stderr, "A simple bridge to connect to remote MCP servers\n\n")

	fmt.Fprintf(os.Stderr, "❌ Error: --url is required\n\n")

	fmt.Fprintf(os.Stderr, "📖 Usage:\n")
	fmt.Fprintf(os.Stderr, "  mcp-remote-go --url=<server-url> [options]\n\n")

	fmt.Fprintf(os.Stderr, "💡 Examples:\n")
	fmt.Fprintf(os.Stderr, "  # Basic connection\n")
	fmt.Fprintf(os.Stderr, "  mcp-remote-go --url=https://mcp.example.com\n\n")

	fmt.Fprintf(os.Stderr, "  # With API key\n")
	fmt.Fprintf(os.Stderr, "  mcp-remote-go --url=https://api.example.com --header='X-API-Key: your-key'\n\n")

	fmt.Fprintf(os.Stderr, "  # SSE endpoint\n")
	fmt.Fprintf(os.Stderr, "  mcp-remote-go --url=https://mcp.example.com/sse\n\n")

	fmt.Fprintf(os.Stderr, "⚙️  Options:\n")
	fmt.Fprintf(os.Stderr, "  --url        Remote MCP server URL (required)\n")
	fmt.Fprintf(os.Stderr, "  --header     Custom headers (repeatable)\n")
	fmt.Fprintf(os.Stderr, "  --transport  Transport strategy: http-first, sse-first, http-only, sse-only\n")
	fmt.Fprintf(os.Stderr, "  --debug      Enable debug logging\n")
	fmt.Fprintf(os.Stderr, "  --allow-http Allow HTTP connections (dev only)\n")
	fmt.Fprintf(os.Stderr, "  --protocol-version     MCP protocol version (default: 2025-06-18)\n")

	fmt.Fprintf(os.Stderr, "\n🔧 Authentication:\n")
	fmt.Fprintf(os.Stderr, "  • No auth        Connect without authentication\n")
	fmt.Fprintf(os.Stderr, "  • Custom headers Use --header for API keys, tokens, etc.\n")

	fmt.Fprintf(os.Stderr, "\n📚 For more information, visit: https://github.com/rishabh-verma/mcp-remote-go\n")
}
