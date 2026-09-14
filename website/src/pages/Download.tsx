import { Downloads } from "../components/Downloads";
import { usePageTitle } from "../lib/utils";

export function DownloadPage() {
  usePageTitle("Binaries — mcp-remote-go");

  return (
    <main>
      <Downloads />
    </main>
  );
}
