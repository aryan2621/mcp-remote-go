package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const callbackTimeout = 5 * time.Minute

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

type dcrResponse struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func (m *Manager) authorize(ctx context.Context, wwwAuth string) error {
	resourceURL, err := url.Parse(m.resource)
	if err != nil {
		return err
	}
	resource := canonicalResource(resourceURL)

	metaURL, headerScope := parseWWWAuthenticate(wwwAuth)
	prm, err := m.fetchProtectedResource(ctx, resourceURL, metaURL)
	if err != nil {
		return err
	}
	if prm.Resource != "" {
		resource = strings.TrimRight(prm.Resource, "/")
	}
	if len(prm.AuthorizationServers) == 0 {
		return fmt.Errorf("protected resource metadata has no authorization_servers")
	}

	asURL, err := url.Parse(prm.AuthorizationServers[0])
	if err != nil {
		return fmt.Errorf("authorization server URL: %w", err)
	}
	as, err := m.fetchAuthServer(ctx, asURL)
	if err != nil {
		return err
	}
	if as.AuthorizationEndpoint == "" || as.TokenEndpoint == "" {
		return fmt.Errorf("authorization server metadata missing endpoints")
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen for oauth callback: %w", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	clientID := m.clientID
	clientSecret := m.clientSecret
	if stored := m.tokens; stored != nil && stored.ClientID != "" && clientID == "" {
		clientID = stored.ClientID
		clientSecret = stored.ClientSecret
	}
	if clientID == "" {
		if as.RegistrationEndpoint == "" {
			return fmt.Errorf("authorization server has no registration_endpoint; pass --client-id")
		}
		reg, err := m.registerClient(ctx, as.RegistrationEndpoint, redirectURI)
		if err != nil {
			return err
		}
		clientID = reg.ClientID
		clientSecret = reg.ClientSecret
	}

	verifier, challenge, err := generatePKCE()
	if err != nil {
		return err
	}
	state, err := randomURLString(16)
	if err != nil {
		return err
	}

	scope := headerScope
	if scope == "" && len(prm.ScopesSupported) > 0 {
		scope = strings.Join(prm.ScopesSupported, " ")
	}

	authURL, err := url.Parse(as.AuthorizationEndpoint)
	if err != nil {
		return err
	}
	q := authURL.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("resource", resource)
	if scope != "" {
		q.Set("scope", scope)
	}
	authURL.RawQuery = q.Encode()

	code, err := waitForCode(ctx, ln, authURL.String(), state, m.log)
	if err != nil {
		return err
	}

	tokens, err := m.exchange(ctx, as.TokenEndpoint, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {clientID},
		"code_verifier": {verifier},
		"resource":      {resource},
	}, clientID, clientSecret)
	if err != nil {
		return err
	}
	tokens.ClientID = clientID
	tokens.ClientSecret = clientSecret
	tokens.TokenURL = as.TokenEndpoint
	tokens.Resource = resource
	return m.save(tokens)
}

func (m *Manager) refresh(ctx context.Context) error {
	if m.tokens == nil || m.tokens.RefreshToken == "" || m.tokens.TokenURL == "" {
		return fmt.Errorf("no refresh token")
	}
	values := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {m.tokens.RefreshToken},
		"client_id":     {m.tokens.ClientID},
	}
	if m.tokens.Resource != "" {
		values.Set("resource", m.tokens.Resource)
	}
	tokens, err := m.exchange(ctx, m.tokens.TokenURL, values, m.tokens.ClientID, m.tokens.ClientSecret)
	if err != nil {
		return err
	}
	if tokens.RefreshToken == "" {
		tokens.RefreshToken = m.tokens.RefreshToken
	}
	tokens.ClientID = m.tokens.ClientID
	tokens.ClientSecret = m.tokens.ClientSecret
	tokens.TokenURL = m.tokens.TokenURL
	tokens.Resource = m.tokens.Resource
	return m.save(tokens)
}

func (m *Manager) fetchProtectedResource(ctx context.Context, resourceURL *url.URL, metaURL string) (*protectedResource, error) {
	var urls []string
	if metaURL != "" {
		urls = append(urls, metaURL)
	}
	urls = append(urls, wellKnownURLs(resourceURL, "oauth-protected-resource")...)
	var last error
	for _, raw := range urls {
		var prm protectedResource
		if err := fetchJSON(ctx, m.http, raw, &prm); err != nil {
			last = err
			continue
		}
		return &prm, nil
	}
	if last == nil {
		last = fmt.Errorf("no protected resource metadata")
	}
	return nil, last
}

func (m *Manager) fetchAuthServer(ctx context.Context, asURL *url.URL) (*authServerMeta, error) {
	urls := wellKnownURLs(asURL, "oauth-authorization-server")
	urls = append(urls, wellKnownURLs(asURL, "openid-configuration")...)
	var last error
	for _, raw := range urls {
		var meta authServerMeta
		if err := fetchJSON(ctx, m.http, raw, &meta); err != nil {
			last = err
			continue
		}
		return &meta, nil
	}
	if last == nil {
		last = fmt.Errorf("no authorization server metadata")
	}
	return nil, last
}

func (m *Manager) registerClient(ctx context.Context, endpoint, redirectURI string) (*dcrResponse, error) {
	payload, err := json.Marshal(map[string]any{
		"client_name":                "mcp-remote-go",
		"redirect_uris":              []string{redirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
		"application_type":           "native",
		"client_uri":                 "https://github.com/aryan2621/mcp-remote-go",
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("dynamic client registration HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var out dcrResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode client registration: %w", err)
	}
	if out.ClientID == "" {
		return nil, fmt.Errorf("dynamic client registration returned no client_id")
	}
	return &out, nil
}

func (m *Manager) exchange(ctx context.Context, tokenURL string, values url.Values, clientID, clientSecret string) (*TokenSet, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(values.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if clientSecret != "" {
		req.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(clientSecret))
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("token endpoint HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var raw tokenResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if raw.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}
	tokens := &TokenSet{
		AccessToken:  raw.AccessToken,
		RefreshToken: raw.RefreshToken,
		TokenType:    raw.TokenType,
	}
	if raw.ExpiresIn > 0 {
		tokens.ExpiresAt = time.Now().Add(time.Duration(raw.ExpiresIn) * time.Second)
	}
	return tokens, nil
}

func waitForCode(ctx context.Context, ln net.Listener, authURL, state string, logf *log.Logger) (string, error) {
	fmt.Fprintf(os.Stderr, "mcp-remote-go: authorization required\nOpen: %s\n", authURL)
	if logf != nil {
		logf.Printf("Opening browser for OAuth")
	}
	_ = openBrowser(authURL)

	ctx, cancel := context.WithTimeout(ctx, callbackTimeout)
	defer cancel()

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("error") != "" {
			msg := q.Get("error")
			if d := q.Get("error_description"); d != "" {
				msg += ": " + d
			}
			http.Error(w, "Authorization failed. You can close this window.", http.StatusBadRequest)
			errCh <- fmt.Errorf("oauth error: %s", msg)
			return
		}
		if q.Get("state") != state {
			http.Error(w, "State mismatch. You can close this window.", http.StatusBadRequest)
			errCh <- fmt.Errorf("oauth state mismatch")
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "Missing code. You can close this window.", http.StatusBadRequest)
			errCh <- fmt.Errorf("oauth callback missing code")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><title>mcp-remote-go</title><p>Authorized. You can close this window.</p>")
		codeCh <- code
	})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		shCtx, shCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shCancel()
		_ = srv.Shutdown(shCtx)
	}()

	select {
	case code := <-codeCh:
		return code, nil
	case err := <-errCh:
		return "", err
	case <-ctx.Done():
		return "", fmt.Errorf("timed out waiting for oauth callback")
	}
}

func openBrowser(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	return cmd.Start()
}
