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

type StreamableHTTPTransport struct {
	url             string
	headers         map[string]string
	client          *http.Client
	debugLog        *log.Logger
	protocolVersion string
	SessionID       string
	sessionMux      sync.Mutex
	sseReader       *bufio.Reader
	sseResp         *http.Response
	sseConnected    bool
	sseConnMux      sync.Mutex
	lastEventID     string
	eventIDMux      sync.Mutex
}

func NewStreamableHTTPTransport(url string, headers map[string]string, protocolVersion string, debugLog *log.Logger) (*StreamableHTTPTransport, error) {
	return &StreamableHTTPTransport{
		url:             url,
		headers:         headers,
		client:          &http.Client{},
		debugLog:        debugLog,
		protocolVersion: protocolVersion,
	}, nil
}

func (t *StreamableHTTPTransport) Connect(ctx context.Context) error {
	t.logDebug("Streamable HTTP: Testing endpoint with POST")
	
	testReq, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewReader([]byte(`{"jsonrpc":"2.0","method":"ping","id":0}`)))
	if err != nil {
		return err
	}

	for k, v := range t.headers {
		testReq.Header.Set(k, v)
	}
	testReq.Header.Set("Content-Type", "application/json")
	testReq.Header.Set("Accept", "application/json, text/event-stream")
	testReq.Header.Set("MCP-Protocol-Version", t.protocolVersion)

	testResp, err := t.client.Do(testReq)
	if err != nil {
		return err
	}
	defer testResp.Body.Close()

	if testResp.StatusCode == 405 || testResp.StatusCode == 404 {
		return &TransportError{
			StatusCode:  testResp.StatusCode,
			Message:     "Streamable HTTP not supported",
			IsRetryable: false,
		}
	}

	sessionID := testResp.Header.Get("Mcp-Session-Id")
	if sessionID != "" {
		t.sessionMux.Lock()
		t.SessionID = sessionID
		t.sessionMux.Unlock()
		t.logDebug("Received session ID: %s", sessionID)
	}

	io.ReadAll(testResp.Body)

	t.logDebug("Streamable HTTP: Opening GET connection for server messages")
	return t.openSSEConnection(ctx)
}

func (t *StreamableHTTPTransport) openSSEConnection(ctx context.Context) error {
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
	}
	t.sessionMux.Unlock()

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return &TransportError{
			StatusCode:  resp.StatusCode,
			Message:     string(body),
			IsRetryable: resp.StatusCode >= 500,
		}
	}

	t.sseResp = resp
	t.sseReader = bufio.NewReader(resp.Body)
	t.sseConnMux.Lock()
	t.sseConnected = true
	t.sseConnMux.Unlock()
	
	t.logDebug("Streamable HTTP: SSE connection established")
	return nil
}

func (t *StreamableHTTPTransport) Send(ctx context.Context, data []byte) error {
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
	if t.SessionID != "" {
		req.Header.Set("Mcp-Session-Id", t.SessionID)
	}
	t.sessionMux.Unlock()

	t.logDebug("Streamable HTTP: Sending POST")
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusNoContent:
		t.logDebug("Successfully sent message (status %d)", resp.StatusCode)
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

func (t *StreamableHTTPTransport) Receive(ctx context.Context) ([]byte, error) {
	t.sseConnMux.Lock()
	if !t.sseConnected {
		t.sseConnMux.Unlock()
		return nil, fmt.Errorf("SSE connection not established")
	}
	t.sseConnMux.Unlock()

	var dataLines []string
	var eventID string

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		line, err := t.sseReader.ReadString('\n')
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

				t.logDebug("Received SSE event: %s", result)
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

func (t *StreamableHTTPTransport) GetSessionID() string {
	t.sessionMux.Lock()
	defer t.sessionMux.Unlock()
	return t.SessionID
}

func (t *StreamableHTTPTransport) TerminateSession(ctx context.Context) error {
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

func (t *StreamableHTTPTransport) Close() error {
	t.sseConnMux.Lock()
	defer t.sseConnMux.Unlock()
	
	if t.sseResp != nil {
		t.sseConnected = false
		return t.sseResp.Body.Close()
	}
	return nil
}

func (t *StreamableHTTPTransport) logDebug(format string, args ...interface{}) {
	if t.debugLog != nil {
		t.debugLog.Printf(format, args...)
	}
}