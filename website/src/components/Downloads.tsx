import { useEffect, useState } from "react";
import { checksumUrl, downloadUrl, type Arch, type Platform } from "../links";

const labels: Record<Platform, string> = {
  linux: "Linux",
  darwin: "macOS",
  windows: "Windows",
};

const rows: { arch: Arch; label: Record<Platform, string> }[] = [
  { arch: "amd64", label: { linux: "x86_64", darwin: "Intel", windows: "x86_64" } },
  { arch: "arm64", label: { linux: "aarch64", darwin: "Apple Silicon", windows: "ARM64" } },
];

const platforms: Platform[] = ["linux", "darwin", "windows"];

function detectPlatform(): Platform {
  const value = navigator.platform.toLowerCase();
  if (value.includes("mac")) return "darwin";
  if (value.includes("win")) return "windows";
  return "linux";
}

export function Downloads() {
  const [platform, setPlatform] = useState<Platform>("linux");

  useEffect(() => {
    setPlatform(detectPlatform());
  }, []);

  return (
    <section className="mx-auto max-w-5xl px-5 py-16">
      <h1 className="text-3xl font-semibold tracking-tight">Binaries</h1>
      <p className="mt-2 max-w-xl text-muted-foreground">
        GitHub Release assets named <span className="font-mono">mcp-remote-go-&lt;os&gt;-&lt;arch&gt;</span>.
        Those files exist only after the release workflow on main publishes the latest tag. Until then these
        links 404.
      </p>

      <div className="mt-8 flex flex-wrap border-b border-border" role="tablist" aria-label="Platform">
        {platforms.map((item) => (
          <button
            key={item}
            type="button"
            role="tab"
            aria-selected={platform === item}
            className={platform === item ? "tab tab-active" : "tab"}
            onClick={() => setPlatform(item)}
          >
            {labels[item]}
          </button>
        ))}
      </div>

      <ul className="mt-6 divide-y divide-border border border-border">
        {rows.map((row) => {
          const name = downloadUrl(platform, row.arch).split("/").pop();
          return (
            <li key={row.arch} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
              <div>
                <p className="font-medium">{row.label[platform]}</p>
                <p className="font-mono text-xs text-muted-foreground">{name}</p>
              </div>
              <div className="flex gap-4 font-mono text-sm">
                <a
                  className="text-accent hover:underline"
                  href={downloadUrl(platform, row.arch)}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  download
                </a>
                <a
                  className="text-muted-foreground hover:text-foreground"
                  href={checksumUrl(platform, row.arch)}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  sha256
                </a>
              </div>
            </li>
          );
        })}
      </ul>
    </section>
  );
}
