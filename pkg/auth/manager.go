package auth

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/rishabh-verma/mcp-remote-go/pkg/config"
)

type Manager struct {
	config     *config.Config
	serverHash string
	debugLog   *log.Logger
}

func NewManager(cfg *config.Config, serverHash string, debugLog *log.Logger) *Manager {
	return &Manager{
		config:     cfg,
		serverHash: serverHash,
		debugLog:   debugLog,
	}
}

func (m *Manager) GetOrRefreshToken(ctx context.Context) (string, error) {
	m.logDebug("Headers: %+v", m.config.Headers)

	if len(m.config.Headers) > 0 {
		for key := range m.config.Headers {
			lowerKey := strings.ToLower(key)
			if lowerKey == "authorization" ||
				strings.HasPrefix(lowerKey, "x-api-") ||
				strings.Contains(lowerKey, "auth") ||
				strings.Contains(lowerKey, "key") {
				m.logDebug("Using custom auth header: %s", key)
				return "", nil
			}
		}
	}

	m.logDebug("No authentication provided, attempting connection without auth")
	return "", nil
}

func (m *Manager) Handle401Response(ctx context.Context, wwwAuthenticate string) (string, error) {
	m.logDebug("Server returned 401, authentication required")
	return "", fmt.Errorf("authentication required - please provide API key or token via --header")
}

func (m *Manager) logDebug(format string, args ...interface{}) {
	if m.debugLog != nil {
		m.debugLog.Printf(format, args...)
	}
}
