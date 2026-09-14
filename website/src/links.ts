export const LINKS = {
  github: "https://github.com/aryan2621/mcp-remote-go",
  releases: "https://github.com/aryan2621/mcp-remote-go/releases/latest",
  goInstall: "github.com/aryan2621/mcp-remote-go@latest",
  downloads: {
    base: "https://github.com/aryan2621/mcp-remote-go/releases/latest/download",
    linuxAmd64: "mcp-remote-go-linux-amd64",
    linuxArm64: "mcp-remote-go-linux-arm64",
    darwinAmd64: "mcp-remote-go-darwin-amd64",
    darwinArm64: "mcp-remote-go-darwin-arm64",
    windowsAmd64: "mcp-remote-go-windows-amd64.exe",
    windowsArm64: "mcp-remote-go-windows-arm64.exe",
  },
} as const;

export type Platform = "linux" | "darwin" | "windows";
export type Arch = "amd64" | "arm64";

const binaries: Record<Platform, Record<Arch, string>> = {
  linux: { amd64: LINKS.downloads.linuxAmd64, arm64: LINKS.downloads.linuxArm64 },
  darwin: { amd64: LINKS.downloads.darwinAmd64, arm64: LINKS.downloads.darwinArm64 },
  windows: { amd64: LINKS.downloads.windowsAmd64, arm64: LINKS.downloads.windowsArm64 },
};

export function downloadUrl(platform: Platform, arch: Arch) {
  return `${LINKS.downloads.base}/${binaries[platform][arch]}`;
}

export function checksumUrl(platform: Platform, arch: Arch) {
  return `${downloadUrl(platform, arch)}.sha256`;
}
