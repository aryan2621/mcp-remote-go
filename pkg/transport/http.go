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
	"time"
)

const (
	defaultRequestTimeout = 60 * time.Second
	incomingBuffer        = 32
)

var requestTimeout = defaultRequestTimeout

func SetRequestTimeout(d time.Duration) {
	if d > 0 {
		requestTimeout = d
	}
}

var wrapHTTP func(http.RoundTripper) http.RoundTripper

func WrapHTTP(fn func(http.RoundTripper) http.RoundTripper) {
	wrapHTTP = fn
}

func newMCPHTTPClient() *http.Client {
	base, ok := http.DefaultTransport.(*http.Transport)
	rt := http.DefaultTransport
	if ok {
		cloned := base.Clone()
		cloned.TLSHandshakeTimeout = 10 * time.Second
		cloned.ResponseHeaderTimeout = 30 * time.Second
		cloned.IdleConnTimeout = 90 * time.Second
		rt = cloned
	}
	if wrapHTTP != nil {
		rt = wrapHTTP(rt)
	}
	return &http.Client{Transport: rt}
}

func withRequestTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, requestTimeout)
}

func RedactHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return headers
	}
	out := make(map[string]string, len(headers))
	for key, value := range headers {
		if isSensitiveHeader(key) {
			out[key] = "[redacted]"
			continue
		}
		out[key] = value
	}
	return out
}

func isSensitiveHeader(key string) bool {
	k := strings.ToLower(key)
	return k == "authorization" ||
		k == "proxy-authorization" ||
		k == "cookie" ||
		k == "set-cookie" ||
		strings.Contains(k, "api-key") ||
		strings.Contains(k, "apikey") ||
		strings.Contains(k, "token") ||
		strings.Contains(k, "secret")
}

type incomingQueue struct {
	ch     chan []byte
	done   chan struct{}
	closed bool
	mu     sync.Mutex
}

func newIncomingQueue() *incomingQueue {
	return &incomingQueue{
		ch:   make(chan []byte, incomingBuffer),
		done: make(chan struct{}),
	}
}

func (q *incomingQueue) push(ctx context.Context, data []byte) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	msg := append([]byte(nil), data...)
	select {
	case q.ch <- msg:
		return nil
	case <-q.done:
		return io.EOF
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (q *incomingQueue) recv(ctx context.Context) ([]byte, error) {
	select {
	case data := <-q.ch:
		return data, nil
	case <-q.done:
		select {
		case data := <-q.ch:
			return data, nil
		default:
			return nil, io.EOF
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (q *incomingQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	close(q.done)
}

type sseEvent struct {
	id    string
	event string
	data  string
}

func readSSEEvent(reader *bufio.Reader) (sseEvent, error) {
	var ev sseEvent
	var dataLines []string

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return ev, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if len(dataLines) == 0 && ev.id == "" && ev.event == "" {
				continue
			}
			ev.data = strings.Join(dataLines, "\n")
			return ev, nil
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "id":
			ev.id = value
		case "event":
			ev.event = value
		case "data":
			dataLines = append(dataLines, value)
		}
	}
}

func enqueueSSEBytes(ctx context.Context, q *incomingQueue, body []byte) error {
	reader := bufio.NewReader(bytes.NewReader(body))
	for {
		ev, err := readSSEEvent(reader)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if ev.data == "" {
			continue
		}
		if err := q.push(ctx, []byte(ev.data)); err != nil {
			return err
		}
	}
}

func captureSessionID(resp *http.Response) string {
	return resp.Header.Get("Mcp-Session-Id")
}

func classifyStatus(status int, body string) *TransportError {
	switch status {
	case http.StatusBadRequest:
		return &TransportError{StatusCode: status, Message: body, IsRetryable: false}
	case http.StatusUnauthorized:
		return &TransportError{StatusCode: status, Message: "authentication required or token invalid", IsRetryable: false}
	case http.StatusForbidden:
		return &TransportError{StatusCode: status, Message: "insufficient permissions", IsRetryable: false}
	case http.StatusNotFound:
		return &TransportError{StatusCode: status, Message: "session expired, need re-initialization", IsRetryable: true}
	case http.StatusMethodNotAllowed:
		return &TransportError{StatusCode: status, Message: "HTTP method not allowed", IsRetryable: false}
	default:
		return &TransportError{StatusCode: status, Message: body, IsRetryable: status >= 500}
	}
}

type HTTPTransport struct {
	url             string
	headers         map[string]string
	client          *http.Client
	debugLog        *log.Logger
	incoming        *incomingQueue
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
		client:          newMCPHTTPClient(),
		debugLog:        debugLog,
		incoming:        newIncomingQueue(),
		protocolVersion: protocolVersion,
	}, nil
}

func (t *HTTPTransport) Connect(ctx context.Context) error {
	t.logDebug("HTTP transport connected to %s", t.url)
	return nil
}

func (t *HTTPTransport) Send(ctx context.Context, data []byte) error {
	reqCtx, cancel := withRequestTimeout(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, t.url, bytes.NewReader(data))
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

	t.logDebug("Sending HTTP request to %s", t.url)
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if sessionID := captureSessionID(resp); sessionID != "" {
		t.sessionMux.Lock()
		t.SessionID = sessionID
		t.sessionMux.Unlock()
		t.logDebug("Received session ID from header: %s", sessionID)
	}

	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		if err := t.incoming.push(ctx, body); err != nil {
			return err
		}
		return nil
	case http.StatusAccepted, http.StatusNoContent:
		t.logDebug("Server processed request (status %d)", resp.StatusCode)
		return nil
	case http.StatusNotFound:
		t.sessionMux.Lock()
		t.SessionID = ""
		t.sessionMux.Unlock()
		return classifyStatus(resp.StatusCode, string(body))
	default:
		return classifyStatus(resp.StatusCode, string(body))
	}
}

func (t *HTTPTransport) Receive(ctx context.Context) ([]byte, error) {
	return t.incoming.recv(ctx)
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

	reqCtx, cancel := withRequestTimeout(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodDelete, t.url, nil)
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
	t.incoming.close()
	return nil
}

func (t *HTTPTransport) logDebug(format string, args ...interface{}) {
	if t.debugLog != nil {
		t.debugLog.Printf(format, args...)
	}
}
