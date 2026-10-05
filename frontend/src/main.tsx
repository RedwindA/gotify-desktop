import "@astryxdesign/core/reset.css";
import "@astryxdesign/core/astryx.css";
import "@astryxdesign/theme-neutral/theme.css";
import "./app.css";

import { InternationalizationProvider } from "@astryxdesign/core/i18n";
import { Theme } from "@astryxdesign/core/theme";
import { neutralTheme } from "@astryxdesign/theme-neutral/built";
import { StrictMode, useEffect, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App";
import { astryxMessages, useLang } from "./i18n";
import { start } from "./store";

/** Gives Astryx's components, and the page, the language the app shows. */
function Localized({ children }: { children: ReactNode }) {
  const lang = useLang();
  useEffect(() => {
    document.documentElement.lang = lang;
  }, [lang]);
  return (
    <InternationalizationProvider locale={lang} messages={astryxMessages}>
      {children}
    </InternationalizationProvider>
  );
}

// ?theme=dark or ?theme=light forces an appearance, for screenshots.
const forced = new URLSearchParams(location.search).get("theme");
const mode = forced === "dark" || forced === "light" ? forced : "system";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Theme theme={neutralTheme} mode={mode}>
      <Localized>
        <App />
      </Localized>
    </Theme>
  </StrictMode>,
);

start().catch((err) => console.error("starting:", err));
