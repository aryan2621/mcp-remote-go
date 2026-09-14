import { Faq } from "../components/Faq";
import { usePageTitle } from "../lib/utils";

export function FaqPage() {
  usePageTitle("FAQ — mcp-remote-go");

  return (
    <main>
      <Faq />
    </main>
  );
}
