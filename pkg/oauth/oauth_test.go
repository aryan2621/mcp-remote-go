package oauth

import (
	"net/url"
	"testing"
)

func TestParseWWWAuthenticate(t *testing.T) {
	header := `Bearer realm="mcp", resource_metadata="https://mcp.example.com/.well-known/oauth-protected-resource", scope="mcp:tools"`
	meta, scope := parseWWWAuthenticate(header)
	if meta != "https://mcp.example.com/.well-known/oauth-protected-resource" {
		t.Fatalf("resource_metadata=%q", meta)
	}
	if scope != "mcp:tools" {
		t.Fatalf("scope=%q", scope)
	}
}

func TestCanonicalResource(t *testing.T) {
	u, err := url.Parse("HTTPS://MCP.Example.com/mcp/")
	if err != nil {
		t.Fatal(err)
	}
	got := canonicalResource(u)
	want := "https://mcp.example.com/mcp"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestGeneratePKCE(t *testing.T) {
	v1, c1, err := generatePKCE()
	if err != nil {
		t.Fatal(err)
	}
	v2, c2, err := generatePKCE()
	if err != nil {
		t.Fatal(err)
	}
	if v1 == "" || c1 == "" || v1 == v2 || c1 == c2 {
		t.Fatalf("expected unique pkce pairs")
	}
}
