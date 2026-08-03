import type { Metadata } from "next";
import type { ReactNode } from "react";

import "./globals.css";

export const metadata: Metadata = {
  title: "FrameOPS — Preview técnico",
  description: "Preview técnico estático do FrameOPS com dados sintéticos.",
};

type RootLayoutProps = Readonly<{
  children: ReactNode;
}>;

export default function RootLayout({ children }: RootLayoutProps) {
  return (
    <html lang="pt-BR">
      <body>
        <div
          aria-hidden="true"
          className="design-contract"
          dangerouslySetInnerHTML={{
            __html: "<!-- THESIS: FrameSeven Ops Deck turns the attack surface and its evidence chain into the central operational reading, refusing the generic card dashboard. OWN-WORLD: near-black petro-teal workbench, electric green scope signal, alert orange review state, hard-edged evidence sheets, dense sans plus mono data. STORY: an operator sees a synthetic engagement, its authorized surface, review state, evidence and delivery limits without a claim of backend activity. FIRST VIEWPORT: fixed command bar and navigation flank a large surface map; the engagement state sits at its upper right, with operational panels directly below. FORM: FrameSeven Ops Deck, brief-pinned direction; seed key 637916fe. FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, and DESIGN.md -->",
          }}
        />
        {children}
      </body>
    </html>
  );
}
