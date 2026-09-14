package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aryan2621/mcp-remote-go/pkg/bridge"
	"github.com/aryan2621/mcp-remote-go/pkg/config"
	"github.com/aryan2621/mcp-remote-go/pkg/oauth"
	"github.com/aryan2621/mcp-remote-go/pkg/transport"
)

var (
	serverURL       = flag.String("url", "", "Remote MCP server URL (required unless set in --config)")
	allowHTTP       = flag.Bool("allow-http", false, "Allow HTTP connections")
	debug           = flag.Bool("debug", false, "Enable debug logging")
	transportFlag   = flag.String("transport", "http-first", "Transport strategy: http-first, sse-first, http-only, sse-only")
	protocolVersion = flag.String("protocol-version", "2025-06-18", "MCP protocol version")
	configPath      = flag.String("config", "", "JSON config file (flags override file values)")
	timeoutSeconds  = flag.Int("timeout", 60, "HTTP request timeout in seconds (does not apply to SSE streams)")
	noOAuth         = flag.Bool("no-oauth", false, "Disable OAuth; use --header only")
	clientID        = flag.String("client-id", "", "OAuth client id when the server has no dynamic registration")
	clientSecret    = flag.String("client-secret", "", "OAuth client secret for confidential clients")
	logout          = flag.Bool("logout", false, "Delete stored OAuth tokens for --url and exit")
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

	cfg := &config.Config{
		Transport:       "http-first",
		ProtocolVersion: "2025-06-18",
		Timeout:         60 * time.Second,
		Headers:         map[string]string{},
		OAuth:           true,
	}

	if path := strings.TrimSpace(*configPath); path != "" {
		fileCfg, err := config.LoadFile(path)
		if err != nil {
			log.Fatalf("Config file: %v", err)
		}
		fileCfg.Apply(cfg)
	}

	setFlags := map[string]bool{}
	flag.Visit(func(f *flag.Flag) {
		setFlags[f.Name] = true
	})

	if setFlags["url"] {
		cfg.ServerURL = *serverURL
	}
	if setFlags["transport"] {
		cfg.Transport = *transportFlag
	}
	if setFlags["protocol-version"] {
		cfg.ProtocolVersion = *protocolVersion
	}
	if setFlags["debug"] {
		cfg.Debug = *debug
	}
	if setFlags["allow-http"] {
		cfg.AllowHTTP = *allowHTTP
	}
	if setFlags["timeout"] {
		cfg.Timeout = time.Duration(*timeoutSeconds) * time.Second
	}
	if setFlags["no-oauth"] && *noOAuth {
		cfg.OAuth = false
	}
	if setFlags["client-id"] {
		cfg.ClientID = *clientID
	}
	if setFlags["client-secret"] {
		cfg.ClientSecret = os.ExpandEnv(*clientSecret)
	}
	if *serverURL != "" && cfg.ServerURL == "" {
		cfg.ServerURL = *serverURL
	}
	for k, v := range parseHeaders(headers) {
		cfg.Headers[k] = v
	}

	if cfg.ServerURL == "" {
		printHelp()
		os.Exit(1)
	}

	if !strings.HasPrefix(cfg.ServerURL, "http://") && !strings.HasPrefix(cfg.ServerURL, "https://") {
		fmt.Fprintf(os.Stderr, "Error: URL must start with http:// or https://\n")
		fmt.Fprintf(os.Stderr, "Got: %s\n", cfg.ServerURL)
		os.Exit(1)
	}

	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".mcp-auth")
	if dir := os.Getenv("MCP_REMOTE_CONFIG_DIR"); dir != "" {
		configDir = dir
	}
	cfg.ConfigDir = configDir

	if cfg.Timeout > 0 {
		transport.SetRequestTimeout(cfg.Timeout)
	}

	if cfg.Debug {
		log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)
	}

	if err := os.MkdirAll(cfg.ConfigDir, 0700); err != nil {
		log.Fatalf("Failed to create config directory: %v", err)
	}

	if *logout {
		mgr, err := oauth.NewManager(cfg.ServerURL, cfg.ConfigDir, "", "", false, nil)
		if err != nil {
			log.Fatalf("Logout: %v", err)
		}
		if err := mgr.Logout(); err != nil {
			log.Fatalf("Logout: %v", err)
		}
		fmt.Fprintf(os.Stderr, "Deleted stored OAuth tokens for %s\n", cfg.ServerURL)
		return
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
	fmt.Fprintf(os.Stderr, "MCP Remote Go\n")
	fmt.Fprintf(os.Stderr, "A stdio bridge to remote MCP servers\n\n")

	fmt.Fprintf(os.Stderr, "Error: --url is required (or set url in --config)\n\n")

	fmt.Fprintf(os.Stderr, "Usage:\n")
	fmt.Fprintf(os.Stderr, "  mcp-remote-go --url=<server-url> [options]\n")
	fmt.Fprintf(os.Stderr, "  mcp-remote-go --config=config.json\n\n")

	fmt.Fprintf(os.Stderr, "Examples:\n")
	fmt.Fprintf(os.Stderr, "  mcp-remote-go --url=https://mcp.example.com\n")
	fmt.Fprintf(os.Stderr, "  mcp-remote-go --url=https://mcp.example.com --header='X-API-Key: your-key'\n")
	fmt.Fprintf(os.Stderr, "  mcp-remote-go --url=https://mcp.example.com --logout\n\n")

	fmt.Fprintf(os.Stderr, "Options:\n")
	fmt.Fprintf(os.Stderr, "  --url               Remote MCP server URL\n")
	fmt.Fprintf(os.Stderr, "  --config            JSON config file\n")
	fmt.Fprintf(os.Stderr, "  --header            Custom headers (repeatable; env vars expanded)\n")
	fmt.Fprintf(os.Stderr, "  --transport         http-first, sse-first, http-only, sse-only\n")
	fmt.Fprintf(os.Stderr, "  --timeout           HTTP request timeout in seconds (default: 60)\n")
	fmt.Fprintf(os.Stderr, "  --debug             Enable debug logging (secrets in headers are redacted)\n")
	fmt.Fprintf(os.Stderr, "  --allow-http        Allow HTTP connections (dev only)\n")
	fmt.Fprintf(os.Stderr, "  --protocol-version  MCP protocol version (default: 2025-06-18)\n")
	fmt.Fprintf(os.Stderr, "  --no-oauth          Disable OAuth; use --header only\n")
	fmt.Fprintf(os.Stderr, "  --client-id         OAuth client id if dynamic registration is unavailable\n")
	fmt.Fprintf(os.Stderr, "  --client-secret     OAuth client secret (confidential clients)\n")
	fmt.Fprintf(os.Stderr, "  --logout            Delete stored OAuth tokens for --url and exit\n\n")

	fmt.Fprintf(os.Stderr, "MCP clients:\n")
	fmt.Fprintf(os.Stderr, "  Spawn this binary over stdio. Use an absolute path if the app cannot find it.\n")
	fmt.Fprintf(os.Stderr, "  Claude Desktop / Cursor / Windsurf / Cline: mcpServers.command + args\n")
	fmt.Fprintf(os.Stderr, "  Claude Code: mcpServers with type=stdio in .mcp.json\n")
	fmt.Fprintf(os.Stderr, "  VS Code: servers.<name> with type=stdio in .vscode/mcp.json\n")
	fmt.Fprintf(os.Stderr, "  Zed: context_servers in settings.json\n")
	fmt.Fprintf(os.Stderr, "  Example: \"command\": \"/absolute/path/to/mcp-remote-go\"\n\n")

	fmt.Fprintf(os.Stderr, "For more information: https://github.com/aryan2621/mcp-remote-go\n")
}
