// Copy as image: Go draws the body of the message with mygo's native UI,
// without a window, and puts it on the clipboard as a PNG. Chat apps rarely
// render markdown, tables least of all, but every one shows an image.
import { useToast } from "@astryxdesign/core/Toast";
import { useEffect, useRef, useSyncExternalStore } from "react";
import { Desktop, type Message } from "./mygo";
import { useT } from "./i18n";
import { errorText } from "./store";

/** A request to copy a message as an image; each request is a new object. */
interface Shot {
  msg: Message;
}

let current: Shot | null = null;
const listeners = new Set<() => void>();

function setCurrent(shot: Shot | null) {
  current = shot;
  for (const l of listeners) l();
}

function subscribe(l: () => void) {
  listeners.add(l);
  return () => listeners.delete(l);
}

/** The request being carried out, or null. */
export function useMessageShot(): Shot | null {
  return useSyncExternalStore(subscribe, () => current);
}

/** Copies a message to the clipboard as an image; one at a time. */
export function copyMessageImage(msg: Message) {
  if (!current) setCurrent({ msg });
}

/** Carries out the requests to copy a message as an image, with a toast for each. */
export function MessageShots() {
  const shot = useMessageShot();
  const toast = useToast();
  const t = useT();
  // StrictMode runs the effect twice; one copy per request.
  const started = useRef<Shot | null>(null);
  useEffect(() => {
    if (!shot || started.current === shot) return;
    started.current = shot;
    const { msg } = shot;
    const dark = window.matchMedia("(prefers-color-scheme: dark)").matches;
    Desktop.copyMessageImage({ serverId: msg.serverId, id: msg.id, dark, scale: window.devicePixelRatio || 1 })
      .then(() => toast({ body: t.copiedAsImage }))
      .catch((err) => toast({ body: t.couldNotCopyImage(errorText(err)), type: "error" }))
      .finally(() => setCurrent(null));
  }, [shot]);
  return null;
}
