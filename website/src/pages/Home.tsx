import { HowItWorks } from "../components/HowItWorks";
import { Hero } from "../components/Hero";
import { usePageTitle } from "../lib/utils";

export function HomePage() {
  usePageTitle("mcp-remote-go");

  return (
    <main>
      <Hero />
      <HowItWorks />
    </main>
  );
}
