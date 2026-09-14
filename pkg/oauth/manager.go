package oauth

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	resource     string
	store        *Store
	key          string
	http         *http.Client
	log          *log.Logger
	clientID     string
	clientSecret string
	locked       bool
	tokens       *TokenSet
	mu           sync.Mutex
}

func NewManager(serverURL, configDir, clientID, clientSecret string, locked bool, debugLog *log.Logger) (*Manager, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("oauth resource URL: %w", err)
	}
	resource := canonicalResource(u)
	m := &Manager{
		resource: resource,
		store:    NewStore(configDir),
		key:      storageKey(resource),
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
		log:          debugLog,
		clientID:     clientID,
		clientSecret: clientSecret,
		locked:       locked,
	}
	tokens, err := m.store.Get(m.key)
	if err != nil {
		return nil, err
	}
	m.tokens = tokens
	return m, nil
}

func (m *Manager) Logout() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens = nil
	err := m.store.Delete(m.key)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *Manager) Wrap(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &authTripper{base: base, m: m}
}

func (m *Manager) apply(req *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locked {
		return
	}
	if m.tokens == nil || m.tokens.AccessToken == "" {
		return
	}
	typ := m.tokens.TokenType
	if typ == "" || strings.EqualFold(typ, "bearer") {
		typ = "Bearer"
	}
	req.Header.Set("Authorization", typ+" "+m.tokens.AccessToken)
}

func (m *Manager) covers(u *url.URL) bool {
	r, err := url.Parse(m.resource)
	if err != nil {
		return false
	}
	if !strings.EqualFold(u.Host, r.Host) {
		return false
	}
	if strings.Contains(u.Path, "/.well-known/") {
		return false
	}
	rp := r.Path
	if rp == "" || rp == "/" {
		return true
	}
	return u.Path == rp || strings.HasPrefix(u.Path, rp+"/")
}

func (m *Manager) recover(ctx context.Context, header http.Header) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locked {
		return fmt.Errorf("authentication required")
	}
	www := header.Get("WWW-Authenticate")
	if m.tokens != nil && m.tokens.RefreshToken != "" {
		if err := m.refresh(ctx); err == nil {
			return nil
		} else if m.log != nil {
			m.log.Printf("OAuth refresh failed: %v", err)
		}
	}
	fmt.Fprintf(os.Stderr, "mcp-remote-go: starting OAuth for %s\n", m.resource)
	return m.authorize(ctx, www)
}

func (m *Manager) save(tokens *TokenSet) error {
	m.tokens = tokens
	return m.store.Set(m.key, tokens)
}

type authTripper struct {
	base http.RoundTripper
	m    *Manager
}

func (t *authTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = rewindable(req)
	if !t.m.covers(req.URL) {
		return t.base.RoundTrip(req)
	}
	t.m.apply(req)
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	if err := t.m.recover(req.Context(), resp.Header); err != nil {
		return nil, err
	}
	retry := req.Clone(req.Context())
	if req.GetBody != nil {
		retry.Body, err = req.GetBody()
		if err != nil {
			return nil, err
		}
	}
	t.m.apply(retry)
	return t.base.RoundTrip(retry)
}

func rewindable(req *http.Request) *http.Request {
	clone := req.Clone(req.Context())
	if req.Body == nil {
		return clone
	}
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err == nil {
			clone.Body = body
			clone.GetBody = req.GetBody
		}
		return clone
	}
	buf, err := io.ReadAll(req.Body)
	_ = req.Body.Close()
	if err != nil {
		clone.Body = http.NoBody
		return clone
	}
	clone.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(buf)), nil
	}
	clone.Body, _ = clone.GetBody()
	clone.ContentLength = int64(len(buf))
	return clone
}
