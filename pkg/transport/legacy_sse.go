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
	"time"
)

const endpointWaitTimeout = 15 * time.Second

type LegacySSETransport struct {
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
	postEndpoint    string
	endpointMux     sync.Mutex
	endpointReady   chan struct{}
	baseURL         string
	incoming        *incomingQueue
	closed          bool
	closeMux        sync.Mutex
	loopGen         uint64
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
		client:          newMCPHTTPClient(),
		debugLog:        debugLog,
		protocolVersion: protocolVersion,
		baseURL:         baseURL,
		incoming:        newIncomingQueue(),
		endpointReady:   make(chan struct{}),
	}, nil
}

func (t *LegacySSETransport) Connect(ctx context.Context) error {
	t.getConnMux.Lock()
	already := t.getConnected
	t.getConnMux.Unlock()
	if already && t.currentPostEndpoint() != "" {
		return nil
	}

	if err := t.openSSEConnection(ctx); err != nil {
		return err
	}
	t.startSSELoop()
	return t.waitForEndpoint(ctx)
}

func (t *LegacySSETransport) openSSEConnection(ctx context.Context) error {
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
	}
	t.eventIDMux.Unlock()

	t.logDebug("Legacy SSE: Connecting to endpoint: %s", t.url)
	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return &TransportError{StatusCode: resp.StatusCode, Message: "endpoint not found", IsRetryable: false}
		}
		return classifyStatus(resp.StatusCode, string(body))
	}

	t.getConnMux.Lock()
	t.getResp = resp
	t.getConnected = true
	t.getConnMux.Unlock()
	t.logDebug("Legacy SSE: Connected successfully")
	return nil
}

func (t *LegacySSETransport) waitForEndpoint(ctx context.Context) error {
	if t.currentPostEndpoint() != "" {
		return nil
	}
	timer := time.NewTimer(endpointWaitTimeout)
	defer timer.Stop()
	select {
	case <-t.endpointReady:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		if t.currentPostEndpoint() != "" {
			return nil
		}
		return fmt.Errorf("timed out waiting for legacy SSE endpoint event")
	}
}

func (t *LegacySSETransport) startSSELoop() {
	t.getConnMux.Lock()
	t.loopGen++
	gen := t.loopGen
	t.getConnMux.Unlock()
	go t.sseLoop(gen)
}

func (t *LegacySSETransport) sseLoop(gen uint64) {
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
				t.logDebug("Legacy SSE: SSE read ended: %v", err)
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
			if ev.event == "endpoint" || strings.HasPrefix(ev.data, "/messages") || t.looksLikeEndpoint(ev.data) {
				t.extractPostEndpoint(ev.data)
				continue
			}
			t.logDebug("Legacy SSE: Received event: %s", ev.data)
			_ = t.incoming.push(context.Background(), []byte(ev.data))
		}

		if t.isClosed() || t.currentLoopGen() != gen {
			return
		}
		time.Sleep(backoff)
		if t.isClosed() || t.currentLoopGen() != gen {
			return
		}
		if backoff < 16*time.Second {
			backoff *= 2
		}
		if err := t.openSSEConnection(context.Background()); err != nil {
			t.logDebug("Legacy SSE: reconnect failed: %v", err)
			continue
		}
		backoff = time.Second
	}
}

func (t *LegacySSETransport) looksLikeEndpoint(message string) bool {
	trimmed := strings.TrimSpace(message)
	return strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") || strings.HasPrefix(trimmed, "/")
}

func (t *LegacySSETransport) extractPostEndpoint(message string) {
	trimmed := strings.TrimSpace(message)
	var postURL string
	switch {
	case strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://"):
		postURL = trimmed
	case strings.HasPrefix(trimmed, "/"):
		postURL = t.baseURL + trimmed
	default:
		return
	}

	t.endpointMux.Lock()
	first := t.postEndpoint == ""
	t.postEndpoint = postURL
	t.endpointMux.Unlock()

	if u, err := neturl.Parse(postURL); err == nil {
		if sessionID := u.Query().Get("session_id"); sessionID != "" {
			t.sessionMux.Lock()
			t.SessionID = sessionID
			t.sessionMux.Unlock()
			t.logDebug("Legacy SSE: Extracted session ID: %s", sessionID)
		}
	}

	t.logDebug("Legacy SSE: Extracted POST endpoint: %s", postURL)
	if first {
		select {
		case <-t.endpointReady:
		default:
			close(t.endpointReady)
		}
	}
}

func (t *LegacySSETransport) Send(ctx context.Context, data []byte) error {
	postURL := t.currentPostEndpoint()
	if postURL == "" {
		if err := t.waitForEndpoint(ctx); err != nil {
			return err
		}
		postURL = t.currentPostEndpoint()
	}
	if postURL == "" {
		return fmt.Errorf("no POST endpoint available, waiting for endpoint message from server")
	}

	reqCtx, cancel := withRequestTimeout(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, postURL, bytes.NewReader(data))
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
	case http.StatusOK:
		if len(bytes.TrimSpace(body)) > 0 {
			return t.incoming.push(ctx, body)
		}
		return nil
	case http.StatusAccepted, http.StatusNoContent:
		t.logDebug("Legacy SSE: Successfully sent message (status %d)", resp.StatusCode)
		return nil
	case http.StatusNotFound:
		t.sessionMux.Lock()
		t.SessionID = ""
		t.sessionMux.Unlock()
		t.endpointMux.Lock()
		t.postEndpoint = ""
		t.endpointReady = make(chan struct{})
		t.endpointMux.Unlock()
		t.getConnMux.Lock()
		t.loopGen++
		t.getConnMux.Unlock()
		t.closeSSE()
		return classifyStatus(resp.StatusCode, string(body))
	default:
		return classifyStatus(resp.StatusCode, string(body))
	}
}

func (t *LegacySSETransport) Receive(ctx context.Context) ([]byte, error) {
	return t.incoming.recv(ctx)
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

func (t *LegacySSETransport) currentPostEndpoint() string {
	t.endpointMux.Lock()
	defer t.endpointMux.Unlock()
	return t.postEndpoint
}

func (t *LegacySSETransport) closeSSE() {
	t.getConnMux.Lock()
	defer t.getConnMux.Unlock()
	t.getConnected = false
	if t.getResp != nil {
		_ = t.getResp.Body.Close()
		t.getResp = nil
	}
}

func (t *LegacySSETransport) currentLoopGen() uint64 {
	t.getConnMux.Lock()
	defer t.getConnMux.Unlock()
	return t.loopGen
}

func (t *LegacySSETransport) isClosed() bool {
	t.closeMux.Lock()
	defer t.closeMux.Unlock()
	return t.closed
}

func (t *LegacySSETransport) logDebug(format string, args ...interface{}) {
	if t.debugLog != nil {
		t.debugLog.Printf(format, args...)
	}
}
