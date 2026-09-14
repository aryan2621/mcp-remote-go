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

type StreamableHTTPTransport struct {
	url             string
	headers         map[string]string
	client          *http.Client
	debugLog        *log.Logger
	protocolVersion string
	SessionID       string
	sessionMux      sync.Mutex
	sseResp         *http.Response
	sseConnected    bool
	sseConnMux      sync.Mutex
	lastEventID     string
	eventIDMux      sync.Mutex
	incoming        *incomingQueue
	closed          bool
	closeMux        sync.Mutex
	loopGen         uint64
	ready           bool
}

func NewStreamableHTTPTransport(url string, headers map[string]string, protocolVersion string, debugLog *log.Logger) (*StreamableHTTPTransport, error) {
	return &StreamableHTTPTransport{
		url:             url,
		headers:         headers,
		client:          newMCPHTTPClient(),
		debugLog:        debugLog,
		protocolVersion: protocolVersion,
		incoming:        newIncomingQueue(),
	}, nil
}

func (t *StreamableHTTPTransport) Connect(ctx context.Context) error {
	t.sseConnMux.Lock()
	already := t.ready
	t.sseConnMux.Unlock()
	if already {
		return nil
	}

	t.sessionMux.Lock()
	needsProbe := t.SessionID == ""
	t.sessionMux.Unlock()

	if needsProbe {
		if err := t.probe(ctx); err != nil {
			return err
		}
	}

	if err := t.openSSEConnection(ctx); err != nil {
		if te, ok := err.(*TransportError); ok && (te.StatusCode == http.StatusMethodNotAllowed || te.StatusCode == http.StatusNotFound) {
			t.logDebug("GET SSE not offered; using POST responses only")
			t.markReady()
			return nil
		}
		return err
	}

	t.startSSELoop()
	t.markReady()
	return nil
}

func (t *StreamableHTTPTransport) probe(ctx context.Context) error {
	t.logDebug("Streamable HTTP: Probing endpoint with POST")

	reqCtx, cancel := withRequestTimeout(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, t.url, bytes.NewReader([]byte(`{"jsonrpc":"2.0","method":"ping","id":0}`)))
	if err != nil {
		return err
	}
	t.applyHeaders(req, "application/json", "application/json, text/event-stream")

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if sessionID := captureSessionID(resp); sessionID != "" {
		t.sessionMux.Lock()
		t.SessionID = sessionID
		t.sessionMux.Unlock()
		t.logDebug("Received session ID: %s", sessionID)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return classifyStatus(resp.StatusCode, string(body))
	}
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotFound {
		return &TransportError{
			StatusCode:  resp.StatusCode,
			Message:     "Streamable HTTP not supported",
			IsRetryable: false,
		}
	}

	// Discard the probe body. Enqueueing it makes Cursor treat the ping
	// result (often id 0 / empty result) as initialize.
	t.logDebug("Streamable HTTP: Probe completed (HTTP %d, %d bytes discarded)", resp.StatusCode, len(body))
	return nil
}

func (t *StreamableHTTPTransport) openSSEConnection(ctx context.Context) error {
	t.closeSSE()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.url, nil)
	if err != nil {
		return err
	}
	t.applyHeaders(req, "", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")

	t.eventIDMux.Lock()
	if t.lastEventID != "" {
		req.Header.Set("Last-Event-ID", t.lastEventID)
		t.logDebug("Resuming with Last-Event-ID: %s", t.lastEventID)
	}
	t.eventIDMux.Unlock()

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return classifyStatus(resp.StatusCode, string(body))
	}

	t.sseConnMux.Lock()
	t.sseResp = resp
	t.sseConnected = true
	t.sseConnMux.Unlock()

	t.logDebug("Streamable HTTP: SSE connection established")
	return nil
}

func (t *StreamableHTTPTransport) startSSELoop() {
	t.sseConnMux.Lock()
	t.loopGen++
	gen := t.loopGen
	t.sseConnMux.Unlock()
	go t.sseLoop(gen)
}

func (t *StreamableHTTPTransport) sseLoop(gen uint64) {
	backoff := time.Second
	for {
		if t.isClosed() || t.currentLoopGen() != gen {
			return
		}

		t.sseConnMux.Lock()
		resp := t.sseResp
		t.sseConnMux.Unlock()
		if resp == nil {
			return
		}

		reader := bufio.NewReader(resp.Body)
		for {
			if t.isClosed() || t.currentLoopGen() != gen {
				return
			}
			ev, err := readSSEEvent(reader)
			if err != nil {
				t.logDebug("Streamable HTTP: SSE read ended: %v", err)
				t.closeSSE()
				break
			}
			if ev.id != "" {
				t.eventIDMux.Lock()
				t.lastEventID = ev.id
				t.eventIDMux.Unlock()
			}
			if ev.data == "" {
				continue
			}
			t.logDebug("Received SSE event: %s", ev.data)
			_ = t.incoming.push(context.Background(), []byte(ev.data))
		}

		if t.isClosed() || t.currentLoopGen() != gen {
			return
		}

		t.logDebug("Streamable HTTP: Reconnecting SSE in %s", backoff)
		time.Sleep(backoff)
		if t.isClosed() || t.currentLoopGen() != gen {
			return
		}
		if backoff < 16*time.Second {
			backoff *= 2
		}
		if err := t.openSSEConnection(context.Background()); err != nil {
			t.logDebug("Streamable HTTP: SSE reconnect failed: %v", err)
			continue
		}
		backoff = time.Second
	}
}

func (t *StreamableHTTPTransport) currentLoopGen() uint64 {
	t.sseConnMux.Lock()
	defer t.sseConnMux.Unlock()
	return t.loopGen
}

func (t *StreamableHTTPTransport) Send(ctx context.Context, data []byte) error {
	reqCtx, cancel := withRequestTimeout(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, t.url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	t.applyHeaders(req, "application/json", "application/json, text/event-stream")

	t.logDebug("Streamable HTTP: Sending POST")
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if sessionID := captureSessionID(resp); sessionID != "" {
		t.sessionMux.Lock()
		t.SessionID = sessionID
		t.sessionMux.Unlock()
	}

	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		ct := resp.Header.Get("Content-Type")
		if strings.Contains(ct, "text/event-stream") {
			return enqueueSSEBytes(ctx, t.incoming, body)
		}
		return t.incoming.push(ctx, body)
	case http.StatusAccepted, http.StatusNoContent:
		t.logDebug("Successfully sent message (status %d)", resp.StatusCode)
		return nil
	case http.StatusNotFound:
		t.resetSession()
		return classifyStatus(resp.StatusCode, string(body))
	default:
		return classifyStatus(resp.StatusCode, string(body))
	}
}

func (t *StreamableHTTPTransport) Receive(ctx context.Context) ([]byte, error) {
	return t.incoming.recv(ctx)
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

	reqCtx, cancel := withRequestTimeout(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodDelete, t.url, nil)
	if err != nil {
		return err
	}
	t.applyHeaders(req, "", "")
	req.Header.Set("Mcp-Session-Id", sessionID)

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
	t.closeMux.Lock()
	t.closed = true
	t.closeMux.Unlock()
	t.closeSSE()
	t.incoming.close()
	return nil
}

func (t *StreamableHTTPTransport) applyHeaders(req *http.Request, contentType, accept string) {
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	req.Header.Set("MCP-Protocol-Version", t.protocolVersion)
	t.sessionMux.Lock()
	if t.SessionID != "" {
		req.Header.Set("Mcp-Session-Id", t.SessionID)
	}
	t.sessionMux.Unlock()
}

func (t *StreamableHTTPTransport) markReady() {
	t.sseConnMux.Lock()
	t.ready = true
	t.sseConnMux.Unlock()
}

func (t *StreamableHTTPTransport) resetSession() {
	t.sessionMux.Lock()
	t.SessionID = ""
	t.sessionMux.Unlock()
	t.sseConnMux.Lock()
	t.loopGen++
	t.ready = false
	t.sseConnMux.Unlock()
	t.closeSSE()
}

func (t *StreamableHTTPTransport) closeSSE() {
	t.sseConnMux.Lock()
	defer t.sseConnMux.Unlock()
	t.sseConnected = false
	if t.sseResp != nil {
		_ = t.sseResp.Body.Close()
		t.sseResp = nil
	}
}

func (t *StreamableHTTPTransport) isClosed() bool {
	t.closeMux.Lock()
	defer t.closeMux.Unlock()
	return t.closed
}

func (t *StreamableHTTPTransport) logDebug(format string, args ...interface{}) {
	if t.debugLog != nil {
		t.debugLog.Printf(format, args...)
	}
}
