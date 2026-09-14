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

type ModernSSETransport struct {
	url             string
	headers         map[string]string
	client          *http.Client
	debugLog        *log.Logger
	getResp         *http.Response
	SessionID       string
	sessionMux      sync.Mutex
	protocolVersion string
	lastEventID     string
	eventIDMux      sync.Mutex
	getConnected    bool
	getConnMux      sync.Mutex
	incoming        *incomingQueue
	closed          bool
	closeMux        sync.Mutex
	loopGen         uint64
}

func NewModernSSETransport(url string, headers map[string]string, protocolVersion string, debugLog *log.Logger) (*ModernSSETransport, error) {
	return &ModernSSETransport{
		url:             url,
		headers:         headers,
		client:          newMCPHTTPClient(),
		debugLog:        debugLog,
		protocolVersion: protocolVersion,
		incoming:        newIncomingQueue(),
	}, nil
}

func (t *ModernSSETransport) Connect(ctx context.Context) error {
	t.getConnMux.Lock()
	already := t.getConnected
	t.getConnMux.Unlock()
	if already {
		return nil
	}

	if err := t.openSSEConnection(ctx); err != nil {
		return err
	}
	t.startSSELoop()
	return nil
}

func (t *ModernSSETransport) openSSEConnection(ctx context.Context) error {
	t.closeSSE()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.url, nil)
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

	t.logDebug("Modern SSE: Connecting to endpoint: %s", t.url)
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			t.sessionMux.Lock()
			t.SessionID = ""
			t.sessionMux.Unlock()
		}
		return classifyStatus(resp.StatusCode, string(body))
	}

	if sessionID := captureSessionID(resp); sessionID != "" {
		t.sessionMux.Lock()
		t.SessionID = sessionID
		t.sessionMux.Unlock()
		t.logDebug("Received session ID from header: %s", sessionID)
	}

	t.getConnMux.Lock()
	t.getResp = resp
	t.getConnected = true
	t.getConnMux.Unlock()
	t.logDebug("Modern SSE: Connected successfully")
	return nil
}

func (t *ModernSSETransport) startSSELoop() {
	t.getConnMux.Lock()
	t.loopGen++
	gen := t.loopGen
	t.getConnMux.Unlock()
	go t.sseLoop(gen)
}

func (t *ModernSSETransport) sseLoop(gen uint64) {
	backoff := time.Second
	for {
		if t.isClosed() || t.currentLoopGen() != gen {
			return
		}
		t.getConnMux.Lock()
		resp := t.getResp
		t.getConnMux.Unlock()
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
				t.logDebug("Modern SSE: SSE read ended: %v", err)
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
			t.logDebug("Modern SSE: Received event: %s", ev.data)
			_ = t.incoming.push(context.Background(), []byte(ev.data))
		}

		if t.isClosed() || t.currentLoopGen() != gen {
			return
		}
		t.logDebug("Modern SSE: Reconnecting in %s", backoff)
		time.Sleep(backoff)
		if t.isClosed() || t.currentLoopGen() != gen {
			return
		}
		if backoff < 16*time.Second {
			backoff *= 2
		}
		if err := t.openSSEConnection(context.Background()); err != nil {
			t.logDebug("Modern SSE: reconnect failed: %v", err)
			continue
		}
		backoff = time.Second
	}
}

func (t *ModernSSETransport) Send(ctx context.Context, data []byte) error {
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

	if newSessionID := captureSessionID(resp); newSessionID != "" && newSessionID != sessionID {
		t.sessionMux.Lock()
		t.SessionID = newSessionID
		t.sessionMux.Unlock()
		t.logDebug("Received new session ID from POST response: %s", newSessionID)
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
		t.logDebug("Modern SSE: Successfully sent message (status %d)", resp.StatusCode)
		return nil
	case http.StatusNotFound:
		t.sessionMux.Lock()
		t.SessionID = ""
		t.sessionMux.Unlock()
		t.getConnMux.Lock()
		t.loopGen++
		t.getConnMux.Unlock()
		t.closeSSE()
		return classifyStatus(resp.StatusCode, string(body))
	default:
		return classifyStatus(resp.StatusCode, string(body))
	}
}

func (t *ModernSSETransport) Receive(ctx context.Context) ([]byte, error) {
	return t.incoming.recv(ctx)
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

func (t *ModernSSETransport) Close() error {
	t.closeMux.Lock()
	t.closed = true
	t.closeMux.Unlock()
	t.getConnMux.Lock()
	t.loopGen++
	t.getConnMux.Unlock()
	t.closeSSE()
	t.incoming.close()
	return nil
}

func (t *ModernSSETransport) closeSSE() {
	t.getConnMux.Lock()
	defer t.getConnMux.Unlock()
	t.getConnected = false
	if t.getResp != nil {
		_ = t.getResp.Body.Close()
		t.getResp = nil
	}
}

func (t *ModernSSETransport) currentLoopGen() uint64 {
	t.getConnMux.Lock()
	defer t.getConnMux.Unlock()
	return t.loopGen
}

func (t *ModernSSETransport) isClosed() bool {
	t.closeMux.Lock()
	defer t.closeMux.Unlock()
	return t.closed
}

func (t *ModernSSETransport) logDebug(format string, args ...interface{}) {
	if t.debugLog != nil {
		t.debugLog.Printf(format, args...)
	}
}
