package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
)

type protectedResource struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

type authServerMeta struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
}

func parseWWWAuthenticate(header string) (resourceMetadata string, scopes string) {
	for _, part := range splitAuthParams(header) {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.Trim(strings.TrimSpace(value), `"`)
		switch key {
		case "resource_metadata":
			resourceMetadata = value
		case "scope":
			scopes = value
		}
	}
	return resourceMetadata, scopes
}

func splitAuthParams(header string) []string {
	header = strings.TrimSpace(header)
	if i := strings.IndexByte(header, ' '); i >= 0 {
		header = strings.TrimSpace(header[i+1:])
	}
	var parts []string
	var b strings.Builder
	inQuote := false
	for _, r := range header {
		switch {
		case r == '"':
			inQuote = !inQuote
			b.WriteRune(r)
		case r == ',' && !inQuote:
			if s := strings.TrimSpace(b.String()); s != "" {
				parts = append(parts, s)
			}
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	if s := strings.TrimSpace(b.String()); s != "" {
		parts = append(parts, s)
	}
	return parts
}

func canonicalResource(u *url.URL) string {
	out := *u
	out.Fragment = ""
	out.RawQuery = ""
	out.Scheme = strings.ToLower(out.Scheme)
	out.Host = strings.ToLower(out.Host)
	if out.Path != "/" {
		out.Path = strings.TrimRight(out.Path, "/")
	}
	return out.String()
}

func fetchJSON(ctx context.Context, client *http.Client, rawURL string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if !strings.Contains(ct, "json") {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("GET %s: HTTP %d content-type %s", rawURL, resp.StatusCode, ct)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: HTTP %d: %s", rawURL, resp.StatusCode, truncate(string(body), 200))
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("decode %s: %w", rawURL, err)
	}
	return nil
}

func wellKnownURLs(base *url.URL, name string) []string {
	origin := &url.URL{Scheme: base.Scheme, Host: base.Host}
	p := strings.Trim(base.Path, "/")
	var urls []string
	if p != "" {
		joined := *base
		joined.Path = path.Join(base.Path, ".well-known", name)
		urls = append(urls, joined.String())
		urls = append(urls, origin.String()+"/.well-known/"+name+"/"+p)
	}
	urls = append(urls, origin.String()+"/.well-known/"+name)
	return unique(urls)
}

func unique(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
