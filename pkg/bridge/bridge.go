package bridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/rishabh-verma/mcp-remote-go/pkg/config"
	"github.com/rishabh-verma/mcp-remote-go/pkg/transport"
)

type Bridge struct {
	Config     *config.Config
	DebugLog   *log.Logger
	ServerHash string
}

func NewBridge(cfg *config.Config) *Bridge {
	hash := HashURL(cfg.ServerURL)

	var debugLog *log.Logger
	if cfg.Debug {
		logFile := filepath.Join(cfg.ConfigDir, fmt.Sprintf("%s_debug.log", hash))
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err == nil {
			debugLog = log.New(io.MultiWriter(os.Stderr, f), "[DEBUG] ", log.LstdFlags|log.Lmicroseconds)
		} else {
			debugLog = log.New(os.Stderr, "[DEBUG] ", log.LstdFlags|log.Lmicroseconds)
		}
	}

	return &Bridge{
		Config:     cfg,
		DebugLog:   debugLog,
		ServerHash: hash,
	}
}

func (b *Bridge) CreateRemoteTransport() (transport.RemoteTransport, error) {
	headers := make(map[string]string)
	for k, v := range b.Config.Headers {
		headers[k] = v
	}

	detector := transport.NewDetector(b.Config.ServerURL, headers, b.DebugLog)
	return detector.DetectTransport(context.Background(), b.Config.ProtocolVersion, b.Config.Transport)
}

func (b *Bridge) ValidateURL() error {
	u, err := url.Parse(b.Config.ServerURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if u.Scheme == "http" && !b.Config.AllowHTTP {
		return fmt.Errorf("HTTP URLs not allowed (use --allow-http flag for trusted networks)")
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL must use http or https scheme")
	}

	return nil
}

func (b *Bridge) LogDebug(format string, args ...interface{}) {
	if b.DebugLog != nil {
		b.DebugLog.Printf(format, args...)
	}
}

func HashURL(serverURL string) string {
	normalizedURL := serverURL
	if len(normalizedURL) > 0 && normalizedURL[len(normalizedURL)-1] == '/' {
		normalizedURL = normalizedURL[:len(normalizedURL)-1]
	}

	h := sha256.Sum256([]byte(normalizedURL))
	return hex.EncodeToString(h[:])[:16]
}

func (b *Bridge) Run(ctx context.Context) error {
	b.LogDebug("Starting mcp-remote-go bridge")
	b.LogDebug("Server URL: %s", b.Config.ServerURL)
	b.LogDebug("Transport strategy: %s", b.Config.Transport)
	b.LogDebug("Protocol version: %s", b.Config.ProtocolVersion)
	b.LogDebug("Headers: %+v", b.Config.Headers)

	if err := b.ValidateURL(); err != nil {
		return err
	}

	remoteTransport, err := b.CreateRemoteTransport()
	if err != nil {
		if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "Unauthorized") {
			b.LogDebug("Received 401 response, authentication required")
			return fmt.Errorf("authentication required - please provide API key or token via --header")
		}
		return err
	}

	proxy := transport.NewProxy(remoteTransport, b.DebugLog)

	b.LogDebug("Starting proxy on stdio")
	return proxy.Run(ctx)
}