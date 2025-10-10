package transport

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	neturl "net/url"
	"strings"
	"sync"
)

type SSETransport struct {
	url             string
	headers         map[string]string
	client          *http.Client
	debugLog        *log.Logger
	reader          *bufio.Reader
	resp            *http.Response
	SessionID       string
	sessionMux      sync.Mutex
	baseURL         string
	protocolVersion string
	lastEventID     string
	eventIDMux      sync.Mutex
}

func NewSSETransport(url string, headers map[string]string, protocolVersion string, debugLog *log.Logger) (*SSETransport, error) {
	u, err := neturl.Parse(url)
	if err != nil {
		return nil, err
	}
	baseURL := fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	return &SSETransport{
		url:             url,
		headers:         headers,
		client:          &http.Client{},
		debugLog:        debugLog,
		baseURL:         baseURL,
		protocolVersion: protocolVersion,
	}, nil
}

func (t *SSETransport) Connect(ctx context.Context) error {
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

	t.logDebug("Connecting to SSE endpoint: %s", t.url)
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

	t.resp = resp
	t.reader = bufio.NewReader(resp.Body)
	t.logDebug("SSE transport connected to %s", t.url)
	return nil
}

func (t *SSETransport) Send(ctx context.Context, data []byte) error {
	t.sessionMux.Lock()
	sessionID := t.SessionID
	t.sessionMux.Unlock()

	if sessionID == "" {
		return fmt.Errorf("no session ID available yet, wait for first SSE message")
	}

	postURL := fmt.Sprintf("%s/messages/?session_id=%s", t.baseURL, sessionID)

	req, err := http.NewRequestWithContext(ctx, "POST", postURL, bytes.NewReader(data))
	if err != nil {
		return err
	}

	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", t.protocolVersion)
	req.Header.Set("Mcp-Session-Id", sessionID)

	t.logDebug("Sending to SSE POST endpoint: %s", postURL)
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

func (t *SSETransport) Receive(ctx context.Context) ([]byte, error) {
	var dataLines []string
	var eventID string

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		line, err := t.reader.ReadString('\n')
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

				if strings.HasPrefix(result, "/messages/?session_id=") {
					t.extractSessionID(result)
					dataLines = nil
					eventID = ""
					continue
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
		} else if strings.HasPrefix(line, "event: ") {
			continue
		}
	}
}

func (t *SSETransport) extractSessionID(message string) {
	parts := strings.Split(message, "session_id=")
	if len(parts) == 2 {
		sessionID := strings.TrimSpace(parts[1])
		t.sessionMux.Lock()
		t.SessionID = sessionID
		t.sessionMux.Unlock()
		t.logDebug("Extracted session ID from message: %s", sessionID)
	}
}

func (t *SSETransport) GetSessionID() string {
	t.sessionMux.Lock()
	defer t.sessionMux.Unlock()
	return t.SessionID
}

func (t *SSETransport) TerminateSession(ctx context.Context) error {
	t.sessionMux.Lock()
	sessionID := t.SessionID
	t.sessionMux.Unlock()

	if sessionID == "" {
		return nil
	}

	deleteURL := t.url
	req, err := http.NewRequestWithContext(ctx, "DELETE", deleteURL, nil)
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

func (t *SSETransport) Close() error {
	if t.resp != nil {
		return t.resp.Body.Close()
	}
	return nil
}

func (t *SSETransport) logDebug(format string, args ...interface{}) {
	if t.debugLog != nil {
		t.debugLog.Printf(format, args...)
	}
}