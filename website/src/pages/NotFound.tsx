import { Link } from "react-router-dom";
import { usePageTitle } from "../lib/utils";

export function NotFoundPage() {
  usePageTitle("Not found — mcp-remote-go");

  return (
    <main className="mx-auto max-w-5xl px-5 py-24">
      <h1 className="text-3xl font-semibold tracking-tight">Not found</h1>
      <p className="mt-2 text-muted-foreground">That route is not on this site.</p>
      <Link to="/" className="btn btn-primary mt-6">
        Home
      </Link>
    </main>
  );
}
