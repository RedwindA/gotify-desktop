// Window chrome the page takes from the system webview: material, accent, and
// the browser behaviours a desktop window should not keep.
import { Desktop, type Appearance } from "./mygo";

const accentStyleId = "system-accent";

function channel(c: number): number {
  const s = c / 255;
  return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
}

/** WCAG relative luminance of a #rrggbb color. */
function luminance(hex: string): number {
  const n = Number.parseInt(hex.slice(1), 16);
  const r = channel((n >> 16) & 255);
  const g = channel((n >> 8) & 255);
  const b = channel(n & 255);
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrast(a: number, b: number): number {
  const [hi, lo] = a >= b ? [a, b] : [b, a];
  return (hi + 0.05) / (lo + 0.05);
}

/** #111111 or white, whichever has the higher WCAG contrast on hex. */
function onAccent(hex: string): "#111111" | "#ffffff" {
  const l = luminance(hex);
  const black = contrast(l, luminance("#111111"));
  const white = contrast(l, 1);
  return black >= white ? "#111111" : "#ffffff";
}

/**
 * Overrides the theme's accent tokens, or removes the override when accent is
 * empty or not #rrggbb. Exposed on window so a preview can try a color.
 */
export function applySystemAccent(accent: string) {
  const existing = document.getElementById(accentStyleId);
  if (!accent) {
    existing?.remove();
    return;
  }
  if (!/^#[0-9a-fA-F]{6}$/.test(accent)) return;
  const hex = accent.toLowerCase();
  const on = onAccent(hex);
  const css = `[data-astryx-theme], [data-astryx-theme] [data-astryx-media] {
    --color-accent: ${hex};
    --color-on-accent: ${on};
    --color-accent-muted: color-mix(in srgb, ${hex} 16%, transparent);
    --color-text-accent: light-dark(color-mix(in srgb, ${hex} 78%, black), color-mix(in srgb, ${hex} 72%, white));
    --color-icon-accent: light-dark(color-mix(in srgb, ${hex} 85%, black), color-mix(in srgb, ${hex} 75%, white));
    --focus-outline-color: ${hex};
    --astryx-theme-neutral-color-status-fill-accent: ${hex};
    --astryx-theme-neutral-color-status-muted-accent: color-mix(in srgb, ${hex} 20%, transparent);
    --shadow-inset-hover: inset 0 0 0 2px color-mix(in srgb, ${hex} 30%, transparent);
    --shadow-inset-selected: inset 0 0 0 2px color-mix(in srgb, ${hex} 50%, transparent);
  }`;
  const style = existing ?? document.createElement("style");
  style.id = accentStyleId;
  style.textContent = css;
  if (!existing) document.head.appendChild(style);
}

function applyAppearance(appearance: Appearance) {
  const root = document.documentElement;
  if (appearance.material) root.dataset.material = appearance.material;
  else delete root.dataset.material;
  applySystemAccent(appearance.accent);
}

async function refreshAppearance() {
  try {
    applyAppearance(await Desktop.systemAppearance());
  } catch {
    // A browser preview has no system appearance; keep the theme.
  }
}

function applyPlatform() {
  const platform = window.mygo?.platform;
  if (!platform) return;
  // The runtime type uses Node's "win32"; the window reports "windows".
  document.documentElement.dataset.platform = platform === "win32" ? "windows" : platform;
}

const textTypes = new Set(["", "text", "search", "url", "email", "password", "tel", "number"]);

function isTextControl(el: EventTarget | null): el is HTMLInputElement | HTMLTextAreaElement {
  if (el instanceof HTMLTextAreaElement) return !el.disabled && !el.readOnly;
  if (el instanceof HTMLInputElement) return !el.disabled && !el.readOnly && textTypes.has(el.type);
  return false;
}

function isTextField(target: EventTarget | null): boolean {
  if (!(target instanceof Element)) return isTextControl(target) || (target instanceof HTMLElement && target.isContentEditable);
  const el = target.closest("input, textarea, [contenteditable]");
  if (!el) return false;
  if (el instanceof HTMLElement && el.isContentEditable) return true;
  return isTextControl(el);
}

/** Spellcheck and autofill are web-page chrome. WebView2 ignores autocomplete=off on passwords. */
function tameField(target: EventTarget | null) {
  const el = target instanceof Element ? target.closest("input, textarea, [contenteditable]") : target;
  if (el instanceof HTMLInputElement && textTypes.has(el.type)) {
    el.spellcheck = false;
    el.autocapitalize = "off";
    el.setAttribute("autocorrect", "off");
    el.autocomplete = el.type === "password" ? "new-password" : "off";
  } else if (el instanceof HTMLTextAreaElement) {
    el.spellcheck = false;
    el.autocapitalize = "off";
    el.setAttribute("autocorrect", "off");
    el.autocomplete = "off";
  } else if (el instanceof HTMLElement && el.isContentEditable) {
    el.spellcheck = false;
    el.setAttribute("autocapitalize", "off");
    el.setAttribute("autocorrect", "off");
  }
}

/** Ctrl/Cmd chords a focused text field uses to edit. Those are never cancelled. */
function editingChord(e: KeyboardEvent): boolean {
  if (!(e.ctrlKey || e.metaKey) || e.altKey || !isTextField(document.activeElement)) return false;
  const key = e.key.toLowerCase();
  return key === "a" || key === "c" || key === "v" || key === "x" || key === "z" || key === "y";
}

function zoomChord(e: KeyboardEvent): boolean {
  if (!(e.ctrlKey || e.metaKey) || e.altKey) return false;
  switch (e.code) {
    case "Equal":
    case "Minus":
    case "Digit0":
    case "NumpadAdd":
    case "NumpadSubtract":
    case "Numpad0":
      return true;
    default:
      return false;
  }
}

function browserChord(e: KeyboardEvent): boolean {
  const mod = (e.ctrlKey || e.metaKey) && !e.altKey;
  const key = e.key.toLowerCase();
  if (mod && (key === "p" || key === "s" || key === "o" || key === "u" || key === "g" || key === "f")) return true;
  if (key === "f3" || key === "f7") return true;
  // Option+arrows move by word in a macOS text field.
  if (e.altKey && (e.key === "ArrowLeft" || e.key === "ArrowRight"))
    return !(document.documentElement.dataset.platform === "darwin" && isTextField(document.activeElement));
  if (e.key === "BrowserBack" || e.key === "BrowserForward") return true;
  if (import.meta.env.PROD && (key === "f5" || (mod && key === "r"))) return true;
  return false;
}

function inFieldOrLink(target: EventTarget | null): boolean {
  return target instanceof Element && target.closest("a[href], input, textarea, [contenteditable]") !== null;
}

function tameTree(node: Node) {
  if (!(node instanceof Element)) return;
  if (node.matches("input, textarea, [contenteditable]")) tameField(node);
  node.querySelectorAll("input, textarea, [contenteditable]").forEach((el) => tameField(el));
}

function installGuards() {
  // Fields are tamed as they appear: a password manager reads autocomplete before focus.
  tameTree(document.body);
  new MutationObserver((records) => {
    for (const record of records) {
      for (const node of record.addedNodes) tameTree(node);
    }
  }).observe(document.body, { childList: true, subtree: true });
  document.addEventListener("focusin", (e) => tameField(e.target), true);
  document.addEventListener("keydown", (e) => {
    if (e.isComposing || editingChord(e)) return;
    if (zoomChord(e) || browserChord(e)) e.preventDefault();
  });
  // ctrlKey is set for pinch-zoom as well as Ctrl+wheel. passive must stay false.
  document.addEventListener(
    "wheel",
    (e) => {
      if (e.ctrlKey) e.preventDefault();
    },
    { passive: false },
  );
  const swallowNavButton = (e: MouseEvent) => {
    if (e.button === 3 || e.button === 4) e.preventDefault();
  };
  document.addEventListener("mousedown", (e) => {
    swallowNavButton(e);
    if (e.button === 1 && !inFieldOrLink(e.target)) e.preventDefault();
  });
  document.addEventListener("mouseup", swallowNavButton);
  document.addEventListener("auxclick", swallowNavButton);
  const swallowFileDrop = (e: DragEvent) => {
    if (!isTextField(e.target)) e.preventDefault();
  };
  document.addEventListener("dragover", swallowFileDrop);
  document.addEventListener("drop", swallowFileDrop);
}

/** Reads the system appearance and stops the webview acting like a browser. */
export function installShell() {
  applyPlatform();
  installGuards();
  void refreshAppearance();
  window.addEventListener("focus", () => void refreshAppearance());
  window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", () => void refreshAppearance());
  window.applySystemAccent = applySystemAccent;
}

declare global {
  interface Window {
    applySystemAccent?: (accent: string) => void;
  }
}
