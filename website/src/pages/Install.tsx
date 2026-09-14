import { InstallGuide } from "../components/InstallGuide";
import { usePageTitle } from "../lib/utils";

export function InstallPage() {
  usePageTitle("Install — mcp-remote-go");

  return (
    <main>
      <InstallGuide />
    </main>
  );
}
