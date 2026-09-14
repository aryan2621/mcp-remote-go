package oauth

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

const keyringService = "mcp-remote-go"

type TokenSet struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	ClientID     string    `json:"client_id,omitempty"`
	ClientSecret string    `json:"client_secret,omitempty"`
	TokenURL     string    `json:"token_url,omitempty"`
	Resource     string    `json:"resource,omitempty"`
}

func (t *TokenSet) AccessValid() bool {
	if t == nil || t.AccessToken == "" {
		return false
	}
	if t.ExpiresAt.IsZero() {
		return true
	}
	return time.Now().Add(30 * time.Second).Before(t.ExpiresAt)
}

type Store struct {
	dir string
}

func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

func (s *Store) Get(key string) (*TokenSet, error) {
	raw, err := keyring.Get(keyringService, key)
	if err == nil {
		return parseTokenSet(raw)
	}
	return s.getFile(key)
}

func (s *Store) Set(key string, tokens *TokenSet) error {
	raw, err := json.Marshal(tokens)
	if err != nil {
		return err
	}
	if err := keyring.Set(keyringService, key, string(raw)); err == nil {
		_ = os.Remove(s.filePath(key))
		return nil
	}
	if s.dir == "" {
		return fmt.Errorf("keychain unavailable and no config dir for token fallback")
	}
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(s.filePath(key), raw, 0600)
}

func (s *Store) Delete(key string) error {
	_ = keyring.Delete(keyringService, key)
	return os.Remove(s.filePath(key))
}

func (s *Store) getFile(key string) (*TokenSet, error) {
	data, err := os.ReadFile(s.filePath(key))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return parseTokenSet(string(data))
}

func (s *Store) filePath(key string) string {
	safe := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' {
			return '-'
		}
		return r
	}, key)
	return filepath.Join(s.dir, "oauth-"+safe+".json")
}

func parseTokenSet(raw string) (*TokenSet, error) {
	var tokens TokenSet
	if err := json.Unmarshal([]byte(raw), &tokens); err != nil {
		return nil, fmt.Errorf("parse stored tokens: %w", err)
	}
	return &tokens, nil
}

func storageKey(resource string) string {
	return hashKey(resource)
}

func hashKey(resource string) string {
	u, err := url.Parse(resource)
	if err == nil {
		resource = canonicalResource(u)
	}
	return "mcp:" + resource
}
