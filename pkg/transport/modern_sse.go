package transport

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
)

type ModernSSETransport struct {
	url             string
	headers         map[string]string
	client          *http.Client
	debugLog        *log.Logger
	getReader       *bufio.Reader
	getResp         *http.Response
	SessionID       string
	sessionMux      sync.Mutex
	protocolVersion string
	lastEventID     string
	eventIDMux      sync.Mutex
	getConnected    bool
	getConnMux      sync.Mutex
}

func NewModernSSETransport(url string, headers map[string]string, protocolVersion string, debugLog *log.Logger) (*ModernSSETransport, error) {
	return &ModernSSETransport{
		url:             url,
		headers:         headers,
		client:          &http.Client{},
		debugLog:        debugLog,
		protocolVersion: protocolVersion,
	}, nil
}

func (t *ModernSSETransport) Connect(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", t.url, nil)
	if err != nil {
		return err
	}

	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("MCP-Protocol-Version", t.protocolVersion)

	t.eventIDMux.Lock()
	if t.lastEventID != "" {
		req.Header.Set("Last-Event-ID", t.lastEventID)
		t.logDebug("Resuming with Last-Event-ID: %s", t.lastEventID)
	}
	t.eventIDMux.Unlock()

	t.sessionMux.Lock()
	if t.SessionID != "" {
		req.Header.Set("Mcp-Session-Id", t.SessionID)
		t.logDebug("Using session ID: %s", t.SessionID)
	}
	t.sessionMux.Unlock()

	t.logDebug("Modern SSE: Connecting to endpoint: %s", t.url)
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}

	switch resp.StatusCode {
	case http.StatusOK:
		break
	case http.StatusUnauthorized:
		resp.Body.Close()
		return &TransportError{
			StatusCode:  resp.StatusCode,
			Message:     "authentication required or token invalid",
			IsRetryable: true,
		}
	case http.StatusForbidden:
		resp.Body.Close()
		return &TransportError{
			StatusCode:  resp.StatusCode,
			Message:     "insufficient permissions",
			IsRetryable: false,
		}
	case http.StatusNotFound:
		resp.Body.Close()
		t.sessionMux.Lock()
		t.SessionID = ""
		t.sessionMux.Unlock()
		return &TransportError{
			StatusCode:  resp.StatusCode,
			Message:     "session expired, need re-initialization",
			IsRetryable: true,
		}
	case http.StatusMethodNotAllowed:
		resp.Body.Close()
		return &TransportError{
			StatusCode:  resp.StatusCode,
			Message:     "SSE not supported at this endpoint",
			IsRetryable: false,
		}
	default:
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return &TransportError{
			StatusCode:  resp.StatusCode,
			Message:     string(body),
			IsRetryable: resp.StatusCode >= 500,
		}
	}

	sessionID := resp.Header.Get("Mcp-Session-Id")
	if sessionID != "" {
		t.sessionMux.Lock()
		t.SessionID = sessionID
		t.sessionMux.Unlock()
		t.logDebug("Received session ID from header: %s", sessionID)
	}

	t.getResp = resp
	t.getReader = bufio.NewReader(resp.Body)
	t.getConnMux.Lock()
	t.getConnected = true
	t.getConnMux.Unlock()
	t.logDebug("Modern SSE: Connected successfully")
	return nil
}

func (t *ModernSSETransport) Send(ctx context.Context, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewReader(data))
	if err != nil {
		return err
	}

	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", t.protocolVersion)

	t.sessionMux.Lock()
	sessionID := t.SessionID
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	t.sessionMux.Unlock()

	t.logDebug("Modern SSE: Sending POST to: %s", t.url)
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	newSessionID := resp.Header.Get("Mcp-Session-Id")
	if newSessionID != "" && newSessionID != sessionID {
		t.sessionMux.Lock()
		t.SessionID = newSessionID
		t.sessionMux.Unlock()
		t.logDebug("Received new session ID from POST response: %s", newSessionID)
	}

	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusNoContent:
		t.logDebug("Modern SSE: Successfully sent message (status %d)", resp.StatusCode)
		return nil

	case http.StatusBadRequest:
		return &TransportError{
			StatusCode:  resp.StatusCode,
			Message:     string(body),
			IsRetryable: false,
		}

	case http.StatusUnauthorized:
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

	default:
		return &TransportError{
			StatusCode:  resp.StatusCode,
			Message:     string(body),
			IsRetryable: resp.StatusCode >= 500,
		}
	}
}

func (t *ModernSSETransport) Receive(ctx context.Context) ([]byte, error) {
	t.getConnMux.Lock()
	if !t.getConnected {
		t.getConnMux.Unlock()
		return nil, fmt.Errorf("SSE connection not established")
	}
	t.getConnMux.Unlock()

	var dataLines []string
	var eventID string

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		line, err := t.getReader.ReadString('\n')
		if err != nil {
			return nil, err
		}

		line = strings.TrimRight(line, "\r\n")

		if line == "" {
			if len(dataLines) > 0 {
				result := strings.Join(dataLines, "\n")

				if eventID != "" {
					t.eventIDMux.Lock()
					t.lastEventID = eventID
					t.eventIDMux.Unlock()
					t.logDebug("Stored event ID: %s", eventID)
				}

				t.logDebug("Modern SSE: Received event: %s", result)
				dataLines = nil
				eventID = ""
				return []byte(result), nil
			}
			continue
		}

		if strings.HasPrefix(line, "id: ") {
			eventID = strings.TrimPrefix(line, "id: ")
		} else if strings.HasPrefix(line, "id:") {
			eventID = strings.TrimPrefix(line, "id:")
		} else if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			dataLines = append(dataLines, data)
		} else if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			dataLines = append(dataLines, data)
		} else if strings.HasPrefix(line, ":") {
			continue
		}
	}
}

func (t *ModernSSETransport) GetSessionID() string {
	t.sessionMux.Lock()
	defer t.sessionMux.Unlock()
	return t.SessionID
}

func (t *ModernSSETransport) TerminateSession(ctx context.Context) error {
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

func (t *ModernSSETransport) Close() error {
	t.getConnMux.Lock()
	defer t.getConnMux.Unlock()

	if t.getResp != nil {
		t.getConnected = false
		return t.getResp.Body.Close()
	}
	return nil
}

func (t *ModernSSETransport) logDebug(format string, args ...interface{}) {
	if t.debugLog != nil {
		t.debugLog.Printf(format, args...)
	}
}