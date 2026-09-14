const hops = [
  {
    n: "01",
    title: "Client",
    body: "Claude Desktop, Cursor, VS Code, Claude Code, Windsurf, Zed, Cline. They never speak HTTP.",
    code: `"command": "/absolute/path/to/mcp-remote-go"`,
  },
  {
    n: "02",
    title: "Bridge",
    body: "Detects Streamable HTTP or SSE, keeps the session, retries on 404, redacts secrets in debug logs.",
    code: `--transport http-first --timeout 60`,
  },
  {
    n: "03",
    title: "Remote",
    body: "Your hosted MCP server. Headers on every request. HTTPS unless you pass --allow-http.",
    code: `--url=https://mcp.example.com --header "X-API-Key: $KEY"`,
  },
];

export function HowItWorks() {
  return (
    <section className="border-y border-border">
      <div className="mx-auto grid max-w-5xl gap-px bg-border px-0 sm:grid-cols-3">
        {hops.map((hop) => (
          <article key={hop.n} className="bg-background px-5 py-10">
            <p className="font-mono text-xs text-accent">{hop.n}</p>
            <h2 className="mt-2 text-xl font-semibold">{hop.title}</h2>
            <p className="mt-2 text-sm text-muted-foreground">{hop.body}</p>
            <pre className="mt-4 overflow-x-auto text-[12px] text-muted-foreground">
              <code>{hop.code}</code>
            </pre>
          </article>
        ))}
      </div>
    </section>
  );
}
