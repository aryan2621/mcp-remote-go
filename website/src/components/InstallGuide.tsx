import { useState } from "react";
import { LINKS, downloadUrl } from "../links";

const samples = {
  go: `go install ${LINKS.goInstall}`,
  darwin: `# Apple Silicon
curl -L ${downloadUrl("darwin", "arm64")} -o mcp-remote-go
chmod +x mcp-remote-go
sudo mv mcp-remote-go /usr/local/bin/

# Intel
curl -L ${downloadUrl("darwin", "amd64")} -o mcp-remote-go
chmod +x mcp-remote-go
sudo mv mcp-remote-go /usr/local/bin/`,
  linux: `# x86_64
curl -L ${downloadUrl("linux", "amd64")} -o mcp-remote-go
chmod +x mcp-remote-go
sudo mv mcp-remote-go /usr/local/bin/

# aarch64
curl -L ${downloadUrl("linux", "arm64")} -o mcp-remote-go
chmod +x mcp-remote-go
sudo mv mcp-remote-go /usr/local/bin/`,
  windows: `# x86_64
Invoke-WebRequest -Uri "${downloadUrl("windows", "amd64")}" -OutFile "mcp-remote-go.exe"

# ARM64
Invoke-WebRequest -Uri "${downloadUrl("windows", "arm64")}" -OutFile "mcp-remote-go.exe"`,
} as const;

const methods = [
  { id: "go", label: "go install" },
  { id: "darwin", label: "macOS" },
  { id: "linux", label: "Linux" },
  { id: "windows", label: "Windows" },
] as const;

const commandNotes: Record<keyof typeof samples, string[]> = {
  go: [
    "Installs to $(go env GOPATH)/bin (often ~/go/bin/mcp-remote-go).",
    "That path is what belongs in command, not /usr/local/bin unless you moved it.",
  ],
  darwin: [
    "These curl URLs are GitHub Release assets. They 404 until the release workflow on main has published the latest tag.",
    "The mv step puts the binary at /usr/local/bin/mcp-remote-go.",
  ],
  linux: [
    "These curl URLs are GitHub Release assets. They 404 until the release workflow on main has published the latest tag.",
    "The mv step puts the binary at /usr/local/bin/mcp-remote-go.",
  ],
  windows: [
    "These URLs are GitHub Release assets. They 404 until the release workflow on main has published the latest tag.",
    "command in client JSON must be the absolute path to mcp-remote-go.exe.",
  ],
};

const clients = {
  claude: {
    label: "Claude Desktop",
    file: "claude_desktop_config.json",
    paths: [
      "macOS: ~/Library/Application Support/Claude/claude_desktop_config.json",
      "Windows: %APPDATA%\\Claude\\claude_desktop_config.json",
    ],
    json: `{
  "mcpServers": {
    "remote": {
      "command": "/absolute/path/to/mcp-remote-go",
      "args": [
        "--url=https://mcp.example.com",
        "--header",
        "X-API-Key: your-api-key"
      ]
    }
  }
}`,
  },
  cursor: {
    label: "Cursor",
    file: "mcp.json",
    paths: ["Project: .cursor/mcp.json", "Global: ~/.cursor/mcp.json"],
    json: `{
  "mcpServers": {
    "remote": {
      "command": "/absolute/path/to/mcp-remote-go",
      "args": [
        "--url=https://mcp.example.com",
        "--header",
        "X-API-Key: your-api-key"
      ]
    }
  }
}`,
  },
  vscode: {
    label: "VS Code",
    file: ".vscode/mcp.json",
    paths: [
      "Workspace: .vscode/mcp.json",
      "User: Command Palette → MCP: Open User Configuration",
    ],
    json: `{
  "servers": {
    "remote": {
      "type": "stdio",
      "command": "/absolute/path/to/mcp-remote-go",
      "args": [
        "--url=https://mcp.example.com",
        "--header",
        "X-API-Key: your-api-key"
      ]
    }
  }
}`,
  },
  claudecode: {
    label: "Claude Code",
    file: ".mcp.json",
    paths: ["Project: .mcp.json", "User: ~/.claude.json (under mcpServers)"],
    json: `{
  "mcpServers": {
    "remote": {
      "type": "stdio",
      "command": "/absolute/path/to/mcp-remote-go",
      "args": [
        "--url=https://mcp.example.com",
        "--header",
        "X-API-Key: your-api-key"
      ]
    }
  }
}`,
  },
  windsurf: {
    label: "Windsurf",
    file: "mcp_config.json",
    paths: ["~/.codeium/windsurf/mcp_config.json"],
    json: `{
  "mcpServers": {
    "remote": {
      "command": "/absolute/path/to/mcp-remote-go",
      "args": [
        "--url=https://mcp.example.com",
        "--header",
        "X-API-Key: your-api-key"
      ]
    }
  }
}`,
  },
  zed: {
    label: "Zed",
    file: "settings.json",
    paths: ["Zed settings → AI → MCP Servers, stored as context_servers"],
    json: `{
  "context_servers": {
    "remote": {
      "command": "/absolute/path/to/mcp-remote-go",
      "args": [
        "--url=https://mcp.example.com",
        "--header",
        "X-API-Key: your-api-key"
      ],
      "env": {}
    }
  }
}`,
  },
  cline: {
    label: "Cline",
    file: "mcp.json",
    paths: [
      "CLI: ~/.cline/mcp.json",
      "Extension: MCP Servers → Configure MCP Servers",
    ],
    json: `{
  "mcpServers": {
    "remote": {
      "command": "/absolute/path/to/mcp-remote-go",
      "args": [
        "--url=https://mcp.example.com",
        "--header",
        "X-API-Key: your-api-key"
      ],
      "disabled": false,
      "autoApprove": []
    }
  }
}`,
  },
} as const;

type Method = keyof typeof samples;
type Client = keyof typeof clients;

export function InstallGuide() {
  const [method, setMethod] = useState<Method>("go");
  const [client, setClient] = useState<Client>("claude");
  const [copied, setCopied] = useState<"cmd" | "cfg" | null>(null);

  async function copy(text: string, which: "cmd" | "cfg") {
    await navigator.clipboard.writeText(text);
    setCopied(which);
    window.setTimeout(() => setCopied(null), 1600);
  }

  const selected = clients[client];

  return (
    <section className="mx-auto max-w-5xl space-y-10 px-5 py-16">
      <div>
        <h1 className="text-3xl font-semibold tracking-tight">Install</h1>
        <p className="mt-2 max-w-xl text-muted-foreground">
          Install the binary, then put that absolute path in your MCP client config. GUI apps often
          cannot see your shell PATH.
        </p>
      </div>

      <div>
        <h2 className="mb-3 text-sm font-medium">Binary</h2>
        <div className="flex flex-wrap border-b border-border" role="tablist" aria-label="Install method">
          {methods.map((item) => (
            <button
              key={item.id}
              type="button"
              role="tab"
              aria-selected={method === item.id}
              className={method === item.id ? "tab tab-active" : "tab"}
              onClick={() => setMethod(item.id)}
            >
              {item.label}
            </button>
          ))}
        </div>
        <div className="mt-4">
          <div className="mb-2 flex items-center justify-between">
            <p className="font-mono text-xs text-muted-foreground">command</p>
            <button type="button" className="btn btn-ghost" onClick={() => void copy(samples[method], "cmd")}>
              {copied === "cmd" ? "Copied" : "Copy"}
            </button>
          </div>
          <pre className="overflow-x-auto rounded-sm border border-border bg-card p-4 text-[13px] leading-6">
            <code>{samples[method]}</code>
          </pre>
          <ul className="mt-3 space-y-1 font-mono text-xs text-muted-foreground">
            {commandNotes[method].map((note) => (
              <li key={note}>{note}</li>
            ))}
          </ul>
        </div>
      </div>

      <div>
        <h2 className="mb-3 text-sm font-medium">Client config</h2>
        <div className="flex flex-wrap border-b border-border" role="tablist" aria-label="MCP client">
          {(Object.keys(clients) as Client[]).map((id) => (
            <button
              key={id}
              type="button"
              role="tab"
              aria-selected={client === id}
              className={client === id ? "tab tab-active" : "tab"}
              onClick={() => setClient(id)}
            >
              {clients[id].label}
            </button>
          ))}
        </div>
        <div className="mt-4">
          <div className="mb-2 flex items-center justify-between">
            <p className="font-mono text-xs text-muted-foreground">{selected.file}</p>
            <button type="button" className="btn btn-ghost" onClick={() => void copy(selected.json, "cfg")}>
              {copied === "cfg" ? "Copied" : "Copy"}
            </button>
          </div>
          <pre className="overflow-x-auto rounded-sm border border-border bg-card p-4 text-[13px] leading-6 text-muted-foreground">
            <code>{selected.json}</code>
          </pre>
          <ul className="mt-3 space-y-1 font-mono text-xs text-muted-foreground">
            {selected.paths.map((path) => (
              <li key={path}>{path}</li>
            ))}
            <li>Replace command with the real absolute path. On Windows use the .exe path.</li>
          </ul>
        </div>
      </div>
    </section>
  );
}
