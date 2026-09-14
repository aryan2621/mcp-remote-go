import { copyFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

const root = path.dirname(fileURLToPath(import.meta.url));
const basePath = "/mcp-remote-go";

function redirectBareBase() {
  const redirect = (
    req: { url?: string },
    res: { statusCode: number; setHeader: (name: string, value: string) => void; end: () => void },
    next: () => void,
  ) => {
    const url = req.url ?? "";
    if (url === basePath || url.startsWith(`${basePath}?`)) {
      res.statusCode = 302;
      res.setHeader("Location", `${basePath}/${url.slice(basePath.length)}`);
      res.end();
      return;
    }
    next();
  };

  return {
    name: "redirect-bare-base",
    configureServer(server: { middlewares: { use: (handler: typeof redirect) => void } }) {
      server.middlewares.use(redirect);
    },
    configurePreviewServer(server: { middlewares: { use: (handler: typeof redirect) => void } }) {
      server.middlewares.use(redirect);
    },
  };
}

export default defineConfig({
  base: `${basePath}/`,
  plugins: [
    react(),
    tailwindcss(),
    redirectBareBase(),
    {
      name: "github-pages-spa",
      closeBundle() {
        copyFileSync(path.resolve(root, "dist/index.html"), path.resolve(root, "dist/404.html"));
      },
    },
  ],
});
