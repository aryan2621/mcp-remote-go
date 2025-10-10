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

type LegacySSETransport struct {
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
	postEndpoint    string
	baseURL         string
}

func NewLegacySSETransport(url string, headers map[string]string, protocolVersion string, debugLog *log.Logger) (*LegacySSETransport, error) {
	u, err := neturl.Parse(url)
	if err != nil {
		return nil, err
	}
	baseURL := fmt.Sprintf("%s://%s", u.Scheme, u.Host)

	return &LegacySSETransport{
		url:             url,
		headers:         headers,
		client:          &http.Client{},
		debugLog:        debugLog,
		protocolVersion: protocolVersion,
		baseURL:         baseURL,
	}, nil
}

func (t *LegacySSETransport) Connect(ctx context.Context) error {
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

	t.logDebug("Legacy SSE: Connecting to endpoint: %s", t.url)
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
		return &TransportError{
			StatusCode:  resp.StatusCode,
			Message:     "endpoint not found",
			IsRetryable: false,
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

	t.getResp = resp
	t.getReader = bufio.NewReader(resp.Body)
	t.getConnMux.Lock()
	t.getConnected = true
	t.getConnMux.Unlock()
	t.logDebug("Legacy SSE: Connected successfully")
	return nil
}

func (t *LegacySSETransport) extractPostEndpoint(message string) {
	if strings.HasPrefix(message, "/messages/?session_id=") || strings.HasPrefix(message, "/messages?session_id=") {
		parts := strings.SplitN(message, "session_id=", 2)
		if len(parts) == 2 {
			sessionID := strings.TrimSpace(parts[1])
			t.sessionMux.Lock()
			t.SessionID = sessionID
			t.sessionMux.Unlock()

			t.postEndpoint = fmt.Sprintf("%s%s", t.baseURL, message)
			t.logDebug("Legacy SSE: Extracted POST endpoint: %s", t.postEndpoint)
			t.logDebug("Legacy SSE: Extracted session ID: %s", sessionID)
		}
	} else if strings.Contains(message, "endpoint") {
		t.postEndpoint = fmt.Sprintf("%s%s", t.baseURL, message)
		t.logDebug("Legacy SSE: Extracted endpoint from event: %s", t.postEndpoint)
	}
}

func (t *LegacySSETransport) Send(ctx context.Context, data []byte) error {
	postURL := t.postEndpoint
	if postURL == "" {
		return fmt.Errorf("no POST endpoint available, waiting for endpoint message from server")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", postURL, bytes.NewReader(data))
	if err != nil {
		return err
	}

	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("MCP-Protocol-Version", t.protocolVersion)

	t.logDebug("Legacy SSE: Sending POST to: %s", postURL)
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusNoContent:
		t.logDebug("Legacy SSE: Successfully sent message (status %d)", resp.StatusCode)
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
		t.postEndpoint = ""
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

func (t *LegacySSETransport) Receive(ctx context.Context) ([]byte, error) {
	t.getConnMux.Lock()
	if !t.getConnected {
		t.getConnMux.Unlock()
		return nil, fmt.Errorf("SSE connection not established")
	}
	t.getConnMux.Unlock()

	var dataLines []string
	var eventID string
	var eventType string

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
					t.logDebug("Legacy SSE: Stored event ID: %s", eventID)
				}

				if eventType == "endpoint" || strings.HasPrefix(result, "/messages") {
					t.extractPostEndpoint(result)
					dataLines = nil
					eventID = ""
					eventType = ""
					continue
				}

				t.logDebug("Legacy SSE: Received event: %s", result)
				dataLines = nil
				eventID = ""
				eventType = ""
				return []byte(result), nil
			}
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimPrefix(line, "event:")
		} else if strings.HasPrefix(line, "id: ") {
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

func (t *LegacySSETransport) GetSessionID() string {
	t.sessionMux.Lock()
	defer t.sessionMux.Unlock()
	return t.SessionID
}

func (t *LegacySSETransport) TerminateSession(ctx context.Context) error {
	t.logDebug("Legacy SSE: Session termination not supported in legacy transport")
	return nil
}

func (t *LegacySSETransport) Close() error {
	t.getConnMux.Lock()
	defer t.getConnMux.Unlock()

	if t.getResp != nil {
		t.getConnected = false
		return t.getResp.Body.Close()
	}
	return nil
}

func (t *LegacySSETransport) logDebug(format string, args ...interface{}) {
	if t.debugLog != nil {
		t.debugLog.Printf(format, args...)
	}
}