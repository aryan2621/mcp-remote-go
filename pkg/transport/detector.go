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

	if strategy == "" || strategy == "http-first" {
		if d.isSSEEndpoint() {
			d.logDebug("URL pattern suggests SSE; using sse-first")
			strategy = "sse-first"
		}
	}

	switch strategy {
	case "http-only":
		return d.tryHTTPOnly(ctx, protocolVersion)
	case "sse-only":
		return d.trySSETransports(ctx, protocolVersion)
	case "sse-first":
		return d.trySSEFirst(ctx, protocolVersion)
	case "http-first":
		fallthrough
	default:
		return d.tryHTTPFirst(ctx, protocolVersion)
	}
}

func (d *Detector) tryHTTPFirst(ctx context.Context, protocolVersion string) (RemoteTransport, error) {
	transport, err := d.tryStreamableHTTP(ctx, protocolVersion)
	if err == nil {
		d.logDebug("Streamable HTTP transport succeeded")
		return transport, nil
	}

	d.logDebug("Streamable HTTP failed (%v), trying SSE transports", err)
	return d.trySSETransports(ctx, protocolVersion)
}

func (d *Detector) trySSEFirst(ctx context.Context, protocolVersion string) (RemoteTransport, error) {
	transport, err := d.trySSETransports(ctx, protocolVersion)
	if err == nil {
		return transport, nil
	}

	d.logDebug("SSE transports failed (%v), trying Streamable HTTP", err)
	return d.tryStreamableHTTP(ctx, protocolVersion)
}

func (d *Detector) tryStreamableHTTP(ctx context.Context, protocolVersion string) (RemoteTransport, error) {
	d.logDebug("Attempting Streamable HTTP transport (MCP 2025-06-18)")

	transport, err := NewStreamableHTTPTransport(d.url, d.headers, protocolVersion, d.debugLog)
	if err != nil {
		return nil, err
	}

	if err := transport.Connect(ctx); err != nil {
		_ = transport.Close()
		if transportErr, ok := err.(*TransportError); ok {
			if transportErr.StatusCode == 405 || transportErr.StatusCode == 404 {
				d.logDebug("Streamable HTTP not supported (HTTP %d)", transportErr.StatusCode)
				return nil, err
			}
		}
		return nil, fmt.Errorf("Streamable HTTP transport failed: %w", err)
	}

	return transport, nil
}

func (d *Detector) trySSETransports(ctx context.Context, protocolVersion string) (RemoteTransport, error) {
	d.logDebug("Attempting SSE-based transports")

	legacyTransport, legacyErr := d.tryLegacySSE(ctx, protocolVersion)
	if legacyErr == nil {
		d.logDebug("Legacy SSE transport (2024-11-05) succeeded")
		return legacyTransport, nil
	}

	d.logDebug("Legacy SSE failed (%v), trying modern SSE", legacyErr)

	modernTransport, modernErr := d.tryModernSSE(ctx, protocolVersion)
	if modernErr == nil {
		d.logDebug("Modern SSE transport succeeded")
		return modernTransport, nil
	}

	return nil, fmt.Errorf("all SSE transports failed - legacy: %v, modern: %v", legacyErr, modernErr)
}

func (d *Detector) tryLegacySSE(ctx context.Context, protocolVersion string) (RemoteTransport, error) {
	d.logDebug("Trying Legacy SSE transport (HTTP+SSE 2024-11-05)")

	transport, err := NewLegacySSETransport(d.url, d.headers, protocolVersion, d.debugLog)
	if err != nil {
		return nil, err
	}

	if err := transport.Connect(ctx); err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("Legacy SSE transport failed: %w", err)
	}

	return transport, nil
}

func (d *Detector) tryModernSSE(ctx context.Context, protocolVersion string) (RemoteTransport, error) {
	d.logDebug("Trying Modern SSE transport (Streamable HTTP with GET)")

	transport, err := NewModernSSETransport(d.url, d.headers, protocolVersion, d.debugLog)
	if err != nil {
		return nil, err
	}

	if err := transport.Connect(ctx); err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("Modern SSE transport failed: %w", err)
	}

	return transport, nil
}

func (d *Detector) tryHTTPOnly(ctx context.Context, protocolVersion string) (RemoteTransport, error) {
	d.logDebug("Trying HTTP-only transport")

	transport, err := NewHTTPTransport(d.url, d.headers, protocolVersion, d.debugLog)
	if err != nil {
		return nil, err
	}

	if err := transport.Connect(ctx); err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("HTTP transport failed: %w", err)
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
	}

	for _, pattern := range ssePatterns {
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
