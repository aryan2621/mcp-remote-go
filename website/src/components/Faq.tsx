const items = [
  {
    q: "What is this?",
    a: "A stdio-to-HTTP bridge. MCP clients spawn it as a local process. It forwards JSON-RPC to a remote MCP server over Streamable HTTP or SSE.",
  },
  {
    q: "How do I authenticate?",
    a: "If the server returns 401, the binary runs MCP OAuth (PKCE, browser login, token refresh) and stores tokens in the OS keychain. Pass --header for API keys. --header Authorization disables OAuth. --no-oauth turns it off. --logout deletes stored tokens.",
  },
  {
    q: "Which transport?",
    a: "Leave http-first unless you know the server. URLs containing /sse switch to sse-first automatically. Override with --transport.",
  },
  {
    q: "The client cannot find the binary.",
    a: "GUI apps often do not inherit your shell PATH. Set command to the absolute path of the binary. After go install that is $(go env GOPATH)/bin/mcp-remote-go. After the curl install steps it is /usr/local/bin/mcp-remote-go.",
  },
  {
    q: "Why is the JSON different per client?",
    a: "Claude Desktop, Cursor, Windsurf, and Cline use a top-level mcpServers object. VS Code uses servers and type stdio. Claude Code uses mcpServers with type stdio in .mcp.json. Zed stores servers under context_servers in settings.json.",
  },
  {
    q: "How do I debug?",
    a: "Pass --debug. Logs go to stderr and ~/.mcp-auth. Authorization and API-key headers are redacted.",
  },
];

export function Faq() {
  return (
    <section className="mx-auto max-w-3xl space-y-8 px-5 py-16">
      <div>
        <h1 className="text-3xl font-semibold tracking-tight">FAQ</h1>
        <p className="mt-2 text-muted-foreground">Usual install blockers.</p>
      </div>
      <div className="divide-y divide-border border-y border-border">
        {items.map((item) => (
          <details key={item.q} className="py-4">
            <summary className="cursor-pointer text-sm font-medium">{item.q}</summary>
            <p className="mt-2 text-sm text-muted-foreground">{item.a}</p>
          </details>
        ))}
      </div>
    </section>
  );
}
