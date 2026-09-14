import { Link } from "react-router-dom";
import { LINKS } from "../links";

export function Footer() {
  return (
    <footer className="mt-auto border-t border-border">
      <div className="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-3 px-5 py-6 text-xs text-muted-foreground">
        <p className="font-mono">mcp-remote-go</p>
        <div className="flex flex-wrap gap-4">
          <Link to="/install" className="hover:text-foreground">
            Install
          </Link>
          <Link to="/download" className="hover:text-foreground">
            Binaries
          </Link>
          <a href={LINKS.releases} target="_blank" rel="noopener noreferrer" className="hover:text-foreground">
            Releases
          </a>
          <a href={LINKS.github} target="_blank" rel="noopener noreferrer" className="hover:text-foreground">
            GitHub
          </a>
        </div>
      </div>
    </footer>
  );
}
