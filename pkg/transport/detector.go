package transport

import (
	"context"
	"fmt"
	"log"
	"strings"
)

type Detector struct {
	url      string
	headers  map[string]string
	debugLog *log.Logger
}

func NewDetector(url string, headers map[string]string, debugLog *log.Logger) *Detector {
	return &Detector{
		url:      url,
		headers:  headers,
		debugLog: debugLog,
	}
}

func (d *Detector) DetectTransport(ctx context.Context, protocolVersion, strategy string) (RemoteTransport, error) {
	d.logDebug("Detecting transport with strategy: %s", strategy)

	if d.isSSEEndpoint() {
		d.logDebug("URL suggests SSE endpoint, trying SSE first")
		if strategy == "http-only" {
			return d.tryHTTP(ctx, protocolVersion)
		}
		return d.trySSEFirst(ctx, protocolVersion)
	}

	if d.isHTTPEndpoint() {
		d.logDebug("URL suggests HTTP/MCP endpoint, trying HTTP first")
		if strategy == "sse-only" {
			return d.trySSE(ctx, protocolVersion)
		}
		return d.tryHTTPFirst(ctx, protocolVersion)
	}

	switch strategy {
	case "http-only":
		return d.tryHTTP(ctx, protocolVersion)
	case "sse-only":
		return d.trySSE(ctx, protocolVersion)
	case "sse-first":
		return d.trySSEFirst(ctx, protocolVersion)
	case "http-first":
		fallthrough
	default:
		return d.tryHTTPFirst(ctx, protocolVersion)
	}
}

func (d *Detector) tryHTTPFirst(ctx context.Context, protocolVersion string) (RemoteTransport, error) {
	transport, err := d.tryHTTP(ctx, protocolVersion)
	if err == nil {
		d.logDebug("HTTP transport succeeded")
		return transport, nil
	}

	d.logDebug("HTTP failed, trying SSE: %v", err)
	return d.trySSE(ctx, protocolVersion)
}

func (d *Detector) trySSEFirst(ctx context.Context, protocolVersion string) (RemoteTransport, error) {
	transport, err := d.trySSE(ctx, protocolVersion)
	if err == nil {
		d.logDebug("SSE transport succeeded")
		return transport, nil
	}

	d.logDebug("SSE failed, trying HTTP: %v", err)
	return d.tryHTTP(ctx, protocolVersion)
}

func (d *Detector) tryHTTP(ctx context.Context, protocolVersion string) (RemoteTransport, error) {
	transport, err := NewHTTPTransport(d.url, d.headers, protocolVersion, d.debugLog)
	if err != nil {
		return nil, err
	}

	if err := transport.Connect(ctx); err != nil {
		return nil, fmt.Errorf("HTTP transport failed: %w", err)
	}

	return transport, nil
}

func (d *Detector) trySSE(ctx context.Context, protocolVersion string) (RemoteTransport, error) {
	transport, err := NewSSETransport(d.url, d.headers, protocolVersion, d.debugLog)
	if err != nil {
		return nil, err
	}

	if err := transport.Connect(ctx); err != nil {
		return nil, fmt.Errorf("SSE transport failed: %w", err)
	}

	return transport, nil
}

func (d *Detector) isSSEEndpoint() bool {
	url := strings.ToLower(d.url)

	ssePatterns := []string{
		"/sse",
		"/events",
		"/stream",
		"/event-stream",
		"/server-sent-events",
		"/sse/",
		"/events/",
		"/stream/",
	}

	for _, pattern := range ssePatterns {
		if strings.Contains(url, pattern) {
			return true
		}
	}

	httpPatterns := []string{
		"/mcp",
		"/api",
		"/http",
		"/mcp/",
		"/api/",
		"/http/",
	}

	for _, pattern := range httpPatterns {
		if strings.Contains(url, pattern) {
			return false
		}
	}

	return false
}

func (d *Detector) isHTTPEndpoint() bool {
	url := strings.ToLower(d.url)

	httpPatterns := []string{
		"/mcp",
		"/api",
		"/http",
		"/mcp/",
		"/api/",
		"/http/",
		"/rpc",
		"/rpc/",
		"/jsonrpc",
		"/jsonrpc/",
	}

	for _, pattern := range httpPatterns {
		if strings.Contains(url, pattern) {
			return true
		}
	}

	return false
}

func (d *Detector) logDebug(format string, args ...interface{}) {
	if d.debugLog != nil {
		d.debugLog.Printf(format, args...)
	}
}
