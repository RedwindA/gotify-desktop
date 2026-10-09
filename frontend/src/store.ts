// The app state from Go, kept up to date by its events, and the page's route.
import { isCallError } from "mygo-runtime";
import { useEffect, useState, useSyncExternalStore } from "react";
import { Desktop, events, type Command, type Navigation, type Server, type Settings, type State } from "./mygo";

let state: State | null = null;
const listeners = new Set<() => void>();

/** Takes a state from Go, unless it is older than the one shown: calls and events race. */
function setState(s: State) {
  if (state && s.gen < state.gen) return;
  state = s;
  for (const l of listeners) l();
  settleDraft();
}

function subscribe(l: () => void) {
  listeners.add(l);
  return () => listeners.delete(l);
}

/** The latest state, or null until it loaded. */
export function useAppState(): State | null {
  return useSyncExternalStore(subscribe, () => state);
}

/** Reloads the state, e.g. after a call whose change the page must see at once. */
export async function refreshState() {
  setState(await Desktop.state());
}

// Routes are kept in the location hash, so that a reload keeps the view:
// #/ all messages, #/s/1 a server, #/s/1/a/2 an app, #/settings.
export type Route =
  | { page: "messages"; serverId: number; appId: number }
  | { page: "settings" };

export function parseRoute(hash: string): Route {
  if (hash === "#/settings") return { page: "settings" };
  const m = /^#\/s\/(\d+)(?:\/a\/(\d+))?$/.exec(hash);
  return { page: "messages", serverId: m ? Number(m[1]) : 0, appId: m?.[2] ? Number(m[2]) : 0 };
}

export function routeHash(r: Route): string {
  if (r.page === "settings") return "#/settings";
  if (r.serverId === 0) return "#/";
  return r.appId === 0 ? `#/s/${r.serverId}` : `#/s/${r.serverId}/a/${r.appId}`;
}

export function go(r: Route) {
  const hash = routeHash(r);
  if (location.hash !== hash) location.hash = hash;
}

// The route outlives the page, which closing the window destroys: a window
// opened again from the tray shows what the last one showed.
const routeKey = "route";

function remember(hash: string) {
  try {
    localStorage.setItem(routeKey, hash);
  } catch {
    // Storage is a convenience here.
  }
}

if (!location.hash || location.hash === "#") {
  try {
    const saved = localStorage.getItem(routeKey);
    if (saved) history.replaceState(null, "", saved);
  } catch {
    // Start on all messages.
  }
}

export function useRoute(): Route {
  const [route, setRoute] = useState(() => parseRoute(location.hash));
  useEffect(() => {
    const on = () => {
      remember(location.hash);
      setRoute(parseRoute(location.hash));
    };
    addEventListener("hashchange", on);
    return () => removeEventListener("hashchange", on);
  }, []);
  return route;
}

// A clicked notification asks to show a message; the messages page highlights it.
let navTarget: Navigation | null = null;
const navListeners = new Set<() => void>();

function navigate(n: Navigation | null) {
  if (!n) return;
  go({ page: "messages", serverId: n.serverId, appId: n.appId });
  // Every notification shows its messages, so the page drops its search even
  // when there is no one message to highlight.
  navTarget = n;
  for (const l of navListeners) l();
}

/** The message a notification asked to show, until takeNavTarget clears it. */
export function useNavTarget(): Navigation | null {
  return useSyncExternalStore(
    (l) => {
      navListeners.add(l);
      return () => navListeners.delete(l);
    },
    () => navTarget,
  );
}

export function clearNavTarget() {
  navTarget = null;
  for (const l of navListeners) l();
}

// A menu command (settings, add server) is taken once, the same way a
// notification's navigation is. It waits here if the page has not subscribed yet.
let pendingCommand: Command | null = null;
const commandListeners = new Set<(command: Command) => void>();

function dispatchCommand(command: Command | null) {
  if (!command) return;
  if (commandListeners.size === 0) {
    pendingCommand = command;
    return;
  }
  for (const listener of commandListeners) listener(command);
}

/** Hears menu commands. The listener is called with one that arrived before it subscribed. */
export function subscribeCommand(listener: (command: Command) => void) {
  commandListeners.add(listener);
  if (pendingCommand) {
    const command = pendingCommand;
    pendingCommand = null;
    listener(command);
  }
  return () => {
    commandListeners.delete(listener);
  };
}

/** Loads the state and listens to Go. Subscribing before the first render catches early events. */
export async function start() {
  events.state.on(setState);
  events.navigate.on(() => void Desktop.takeNavigation().then(navigate));
  events.command.on(() => void Desktop.takeCommand().then(dispatchCommand));
  const [s, n, c] = await Promise.all([Desktop.state(), Desktop.takeNavigation(), Desktop.takeCommand()]);
  setState(s);
  navigate(n);
  dispatchCommand(c);
}

/** The text of an error from Go, or of any other error. */
export function errorText(err: unknown): string {
  if (isCallError(err) || err instanceof Error) return err.message;
  return String(err);
}

export function findServer(s: State, id: number): Server | undefined {
  return s.servers.find((sv) => sv.id === id);
}

/** Ticks every interval while mounted, for countdowns and relative times. */
export function useNow(interval: number): number {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), interval);
    return () => clearInterval(t);
  }, [interval]);
  return now;
}

// App images are fetched once per image and kept as data URLs.
const images = new Map<string, Promise<string>>();

export function useAppImage(serverId: number, appId: number, imageKey: string): string | undefined {
  const key = `${serverId}/${appId}/${imageKey}`;
  const [src, setSrc] = useState<{ key: string; url: string }>();
  useEffect(() => {
    if (!imageKey) return;
    let p = images.get(key);
    if (!p) {
      p = Desktop.appImage(serverId, appId).catch(() => "");
      images.set(key, p);
    }
    let live = true;
    void p.then((url) => live && setSrc({ key, url }));
    return () => {
      live = false;
    };
  }, [key, serverId, appId, imageKey]);
  return imageKey && src?.key === key && src.url ? src.url : undefined;
}

// Settings edits show at once and save right away, numbered from an epoch Go
// gives this page, so that Go keeps the newest edit whatever order the saves
// arrive in, and a page loaded later wins. The draft shows until Go's state
// holds the last edit.
let epoch: Promise<number> | null = null;
let edits = 0;
let lastSeq = 0;
let settingsDraft: Settings | null = null;
let pendingSaves = 0;
const draftListeners = new Set<() => void>();

function setDraft(d: Settings | null) {
  settingsDraft = d;
  for (const l of draftListeners) l();
}

function settleDraft() {
  if (settingsDraft && pendingSaves === 0 && state && state.settingsSeq >= lastSeq) setDraft(null);
}

/** The settings as edited, and a function that edits and saves them. */
export function useSettings(saved: Settings, onError: (msg: string) => void) {
  const draft = useSyncExternalStore(
    (l) => {
      draftListeners.add(l);
      return () => draftListeners.delete(l);
    },
    () => settingsDraft,
  );
  const update = (change: Partial<Settings>) => {
    const next = { ...(settingsDraft ?? saved), ...change };
    epoch ??= Desktop.settingsEpoch();
    const n = ++edits;
    setDraft(next);
    pendingSaves++;
    let seq = 0;
    epoch
      .then((e) => {
        seq = e * 2 ** 20 + n;
        lastSeq = seq;
        return Desktop.setSettings(seq, next);
      })
      .catch((err) => {
        onError(errorText(err));
        // A save that never reached Go must not keep the draft forever.
        if (seq === 0) epoch = null; // no epoch: ask again next time
        else if (lastSeq === seq) lastSeq = 0;
      })
      .finally(() => {
        pendingSaves--;
        void refreshState()
          .catch(() => {})
          .then(settleDraft);
      });
  };
  return [draft ?? saved, update] as const;
}
