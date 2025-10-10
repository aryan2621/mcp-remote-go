package transport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
)

type HTTPTransport struct {
	url             string
	headers         map[string]string
	client          *http.Client
	debugLog        *log.Logger
	respChan        chan []byte
	protocolVersion string
	SessionID       string
	sessionMux      sync.Mutex
}

type TransportError struct {
	StatusCode  int
	Message     string
	IsRetryable bool
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}

func NewHTTPTransport(url string, headers map[string]string, protocolVersion string, debugLog *log.Logger) (*HTTPTransport, error) {
	return &HTTPTransport{
		url:             url,
		headers:         headers,
		client:          &http.Client{},
		debugLog:        debugLog,
		respChan:        make(chan []byte, 10),
		protocolVersion: protocolVersion,
	}, nil
}

func (t *HTTPTransport) Connect(ctx context.Context) error {
	t.logDebug("HTTP transport connected to %s", t.url)
	return nil
}

func (t *HTTPTransport) Send(ctx context.Context, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewReader(data))
	if err != nil {
		return err
	}

	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("MCP-Protocol-Version", t.protocolVersion)

	t.sessionMux.Lock()
	if t.SessionID != "" {
		req.Header.Set("Mcp-Session-Id", t.SessionID)
	}
	t.sessionMux.Unlock()

	t.logDebug("Sending HTTP request to %s", t.url)
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	sessionID := resp.Header.Get("Mcp-Session-Id")
	if sessionID != "" {
		t.sessionMux.Lock()
		t.SessionID = sessionID
		t.sessionMux.Unlock()
		t.logDebug("Received session ID from header: %s", sessionID)
	}

	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		if len(body) > 0 {
			select {
			case t.respChan <- body:
				t.logDebug("HTTP response queued: %s", string(body))
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil

	case http.StatusAccepted, http.StatusNoContent:
		t.logDebug("Server processed request (status %d)", resp.StatusCode)
		return nil

	case http.StatusBadRequest:
		return &TransportError{
			StatusCode:  resp.StatusCode,
			Message:     string(body),
			IsRetryable: false,
		}

	case http.StatusUnauthorized:
		t.logDebug("Authentication required (401)")
		return &TransportError{
			StatusCode:  resp.StatusCode,
			Message:     "authentication required or token invalid",
			IsRetryable: true,
		}

	case http.StatusForbidden:
		return &TransportError{
			StatusCode:  resp.StatusCode,
			Message:     "insufficient permissions",
			IsRetryable: false,
		}

	case http.StatusNotFound:
		t.sessionMux.Lock()
		t.SessionID = ""
		t.sessionMux.Unlock()
		return &TransportError{
			StatusCode:  resp.StatusCode,
			Message:     "session expired, need re-initialization",
			IsRetryable: true,
		}

	case http.StatusMethodNotAllowed:
		return &TransportError{
			StatusCode:  resp.StatusCode,
			Message:     "HTTP method not allowed - this endpoint may require SSE transport",
			IsRetryable: false,
		}

	default:
		return &TransportError{
			StatusCode:  resp.StatusCode,
			Message:     string(body),
			IsRetryable: resp.StatusCode >= 500,
		}
	}
}

func (t *HTTPTransport) Receive(ctx context.Context) ([]byte, error) {
	select {
	case data := <-t.respChan:
		return data, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (t *HTTPTransport) GetSessionID() string {
	t.sessionMux.Lock()
	defer t.sessionMux.Unlock()
	return t.SessionID
}

func (t *HTTPTransport) TerminateSession(ctx context.Context) error {
	t.sessionMux.Lock()
	sessionID := t.SessionID
	t.sessionMux.Unlock()

	if sessionID == "" {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, "DELETE", t.url, nil)
	if err != nil {
		return err
	}

	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Mcp-Session-Id", sessionID)
	req.Header.Set("MCP-Protocol-Version", t.protocolVersion)

	t.logDebug("Terminating session: %s", sessionID)
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusMethodNotAllowed {
		t.logDebug("Server does not support explicit session termination")
		return nil
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("session termination failed: HTTP %d: %s", resp.StatusCode, body)
	}

	t.logDebug("Session terminated successfully")
	return nil
}

func (t *HTTPTransport) Close() error {
	close(t.respChan)
	return nil
}

func (t *HTTPTransport) logDebug(format string, args ...interface{}) {
	if t.debugLog != nil {
		t.debugLog.Printf(format, args...)
	}
}