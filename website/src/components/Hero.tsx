import { Link } from "react-router-dom";

const sample = `$ mcp-remote-go \\
    --url=https://mcp.example.com \\
    --header "Authorization: Bearer $TOKEN"

stdin   {"jsonrpc":"2.0","id":1,"method":"tools/list"}
POST    https://mcp.example.com
SSE     data: {"jsonrpc":"2.0","id":1,"result":{…}}
stdout  {"jsonrpc":"2.0","id":1,"result":{…}}`;

export function Hero() {
  return (
    <section className="mx-auto grid max-w-5xl gap-10 px-5 py-16 lg:grid-cols-2 lg:items-center">
      <div>
        <p className="font-mono text-xs tracking-widest text-accent uppercase">stdio bridge</p>
        <h1 className="mt-3 text-4xl font-semibold tracking-tight sm:text-5xl">
          Local MCP clients, remote HTTP servers.
        </h1>
        <p className="mt-4 max-w-md text-muted-foreground">
          Claude Desktop, Cursor, VS Code, Claude Code, and other stdio MCP clients spawn a process. This
          binary is that process. It speaks JSON-RPC on stdin and HTTP or SSE on the wire.
        </p>
        <div className="mt-6 flex flex-wrap gap-2">
          <Link to="/download" className="btn btn-primary">
            Get binary
          </Link>
          <Link to="/install" className="btn btn-outline">
            Install
          </Link>
        </div>
      </div>
      <pre className="overflow-x-auto rounded-sm border border-border bg-card p-4 text-[13px] leading-6 text-muted-foreground">
        <code>{sample}</code>
      </pre>
    </section>
  );
}
