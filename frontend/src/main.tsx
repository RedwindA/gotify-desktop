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
import { start, useAppState } from "./store";

// Keep native editing commands in fields, and the browser menu for development.
// Only cancel the default action: Astryx's message menus still receive the event.
if (import.meta.env.PROD) {
  document.addEventListener("contextmenu", (event) => {
    const target = event.target;
    if (target instanceof HTMLElement && (target.closest("input, textarea") || target.isContentEditable)) return;
    event.preventDefault();
  });
}

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

// Thin scrollbars drawn by app.css, except on macOS, whose own already are.
if (!/Mac/.test(navigator.userAgent)) document.documentElement.classList.add("custom-scrollbars");

// ?theme=dark or ?theme=light forces an appearance, for screenshots.
const forced = new URLSearchParams(location.search).get("theme");

/** The appearance the settings choose. In the app Go also gives it to the
 * webview, so the title bar and prefers-color-scheme follow; a browser showing
 * the preview only gets it from here. */
function Themed({ children }: { children: ReactNode }) {
  const chosen = useAppState()?.settings.theme;
  const mode = forced === "dark" || forced === "light" ? forced : chosen || "system";
  useEffect(() => {
    document.documentElement.style.colorScheme = mode === "system" ? "" : mode;
  }, [mode]);
  return (
    <Theme theme={neutralTheme} mode={mode}>
      {children}
    </Theme>
  );
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <Themed>
      <Localized>
        <App />
      </Localized>
    </Themed>
  </StrictMode>,
);

start().catch((err) => console.error("starting:", err));
