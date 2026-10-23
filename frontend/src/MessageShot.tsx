// Copy as image: the message is laid out on a stage over the window, captured
// by Go a viewport at a time and put on the clipboard as one PNG. Chat apps
// rarely render markdown, tables least of all, but every one shows an image.
import { useToast } from "@astryxdesign/core/Toast";
import { VStack } from "@astryxdesign/core/Layout";
import { useEffect, useRef, useSyncExternalStore } from "react";
import { MessageContent } from "./MessageView";
import { Desktop, type Message } from "./mygo";
import { useT } from "./i18n";
import { errorText, findServer, useAppState } from "./store";

/** The width of the image in CSS pixels, when the window is wider. */
const shotWidth = 640;

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

const frames = () => new Promise<void>((r) => requestAnimationFrame(() => requestAnimationFrame(() => r())));
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

/** Waits until the images in el loaded or failed, for at most 5 seconds. */
async function imagesSettled(el: HTMLElement) {
  const pending = [...el.querySelectorAll("img")].filter((img) => !img.complete);
  const settled = Promise.all(
    pending.map((img) => new Promise((r) => (img.addEventListener("load", r, { once: true }), img.addEventListener("error", r, { once: true })))),
  );
  await Promise.race([settled, sleep(5000)]);
}

async function capture(card: HTMLElement) {
  await document.fonts.ready;
  await imagesSettled(card);
  await frames();
  const { width, height } = card.getBoundingClientRect();
  const viewportWidth = window.innerWidth;
  const viewport = window.innerHeight;
  const h = Math.ceil(height);
  await Desktop.beginImage(width, h);
  for (let top = 0; top < h; top += viewport) {
    card.style.transform = `translateY(${-top}px)`;
    await frames();
    await sleep(30);
    await Desktop.captureSlice({ x: 0, y: 0, width, height: Math.min(viewport, h - top), toY: top, viewportWidth });
  }
  await Desktop.copyImage();
}

/** Lays out the message being copied as an image over the window, and copies it. */
export function MessageShotStage() {
  const shot = useMessageShot();
  const state = useAppState();
  const toast = useToast();
  const t = useT();
  const card = useRef<HTMLDivElement>(null);
  // StrictMode runs the effect twice; one capture per request.
  const started = useRef<Shot | null>(null);
  useEffect(() => {
    if (!shot || !card.current || started.current === shot) return;
    started.current = shot;
    // The stage covers the window, so it never stays up for long.
    const timeout = sleep(30000).then(() => Promise.reject(new Error(t.timedOut)));
    Promise.race([capture(card.current), timeout])
      .then(() => toast({ body: t.copiedAsImage }))
      .catch((err) => toast({ body: t.couldNotCopyImage(errorText(err)), type: "error" }))
      .finally(() => setCurrent(null));
  }, [shot]);
  if (!shot) return null;
  const { msg } = shot;
  const server = state ? findServer(state, msg.serverId) : undefined;
  const app = server?.apps.find((a) => a.id === msg.appId);
  const serverName = state && state.servers.length > 1 ? server?.name : undefined;
  return (
    <div className="msg-shot-stage" aria-hidden>
      <div ref={card} className="msg-shot" style={{ width: Math.min(window.innerWidth, shotWidth) }}>
        <VStack gap={4}>
          <MessageContent msg={msg} app={app} serverName={serverName} />
        </VStack>
      </div>
    </div>
  );
}
