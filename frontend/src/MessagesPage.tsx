import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { HStack, Layout, LayoutContent, LayoutHeader, VStack } from "@astryxdesign/core/Layout";
import { MobileNavToggle } from "@astryxdesign/core/MobileNav";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Spinner } from "@astryxdesign/core/Spinner";
import { TextInput } from "@astryxdesign/core/TextInput";
import { ArrowLeftIcon, CheckCheckIcon, InboxIcon, SearchIcon, SearchXIcon } from "lucide-react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from "react";
import { openMessageLink } from "./MessageCard";
import { MessageRow } from "./MessageRow";
import { MessageView } from "./MessageView";
import { Desktop, type Message, type MessagePage, type MessageRef, type Server, type State } from "./mygo";
import { useT } from "./i18n";
import { statusText } from "./Sidebar";
import { clearNavTarget, errorText, findServer, useNavTarget, useNow } from "./store";

const pageSize = 100;
const markReadDelay = 1200;
// Rows on screen, plus a few around them, so the page's memory does not grow
// with the messages it went through.
const estimatedRowHeight = 72;
const rowOverscan = 6;
// The next page loads when the rows rendered reach this close to the end.
const loadMoreAhead = 10;
// The list and the reading pane share the page once it is at least this wide.
const splitAt = 720;

const isMac = /Mac/.test(navigator.userAgent);

const keyOf = (m: { serverId: number; id: number }) => `${m.serverId}/${m.id}`;

/** Keeps the objects of messages that did not change, so their rows do not render again. */
function reconcile(prev: Message[], next: Message[]): Message[] {
  const old = new Map(prev.map((m) => [keyOf(m), m]));
  return next.map((m) => {
    const o = old.get(keyOf(m));
    return o && o.read === m.read && o.title === m.title && o.body === m.body && o.date === m.date ? o : m;
  });
}

/** The window's height, kept up to date. */
function useViewportHeight(): number {
  const [h, setH] = useState(innerHeight);
  useEffect(() => {
    const on = () => setH(innerHeight);
    addEventListener("resize", on);
    return () => removeEventListener("resize", on);
  }, []);
  return h;
}

/**
 * Marks an unread message read once its row stayed on screen for a moment in
 * a focused window. A row counts as on screen once any of it reaches the
 * upper three quarters of the window. shown names the rows rendered, which
 * change as the list scrolls.
 */
function useMarkVisibleRead(list: HTMLElement | null, messages: Message[], shown: string, onError: (message: string) => void) {
  const latest = useRef(messages);
  latest.current = messages;
  const observer = useRef<IntersectionObserver | null>(null);
  // When each row on screen came into view, while the window had the focus.
  const since = useRef(new Map<string, number>());
  const observed = useRef(new Set<HTMLElement>());
  const height = useViewportHeight();
  useEffect(() => {
    if (!list) return;
    const seen = since.current;
    const marked = new Set<string>();
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          const k = (e.target as HTMLElement).dataset.key!;
          if (!e.isIntersecting) seen.delete(k);
          else if (!seen.has(k)) seen.set(k, performance.now());
        }
      },
      { rootMargin: `0px 0px -${Math.round(height / 4)}px 0px` },
    );
    observer.current = io;
    for (const el of observed.current) io.observe(el);
    // Time in the background does not count, even when the webview suspended the tick.
    const restart = () => {
      const now = performance.now();
      for (const k of seen.keys()) seen.set(k, now);
    };
    addEventListener("focus", restart);
    document.addEventListener("visibilitychange", restart);
    const tick = setInterval(() => {
      const now = performance.now();
      if (!document.hasFocus() || document.hidden) {
        for (const k of seen.keys()) seen.set(k, now);
        return;
      }
      const byServer = new Map<number, number[]>();
      for (const m of latest.current) {
        const k = keyOf(m);
        const t = seen.get(k);
        if (m.read || marked.has(k) || t === undefined || now - t < markReadDelay) continue;
        marked.add(k);
        byServer.set(m.serverId, [...(byServer.get(m.serverId) ?? []), m.id]);
      }
      for (const [server, ids] of byServer) {
        void Desktop.markRead(server, ids).catch((err) => {
          for (const id of ids) {
            const key = keyOf({ serverId: server, id });
            marked.delete(key);
            seen.set(key, performance.now() + 5000);
          }
          onError(errorText(err));
        });
      }
    }, 200);
    return () => {
      io.disconnect();
      observer.current = null;
      seen.clear();
      clearInterval(tick);
      removeEventListener("focus", restart);
      document.removeEventListener("visibilitychange", restart);
    };
  }, [list, height, onError]);
  // Observes the unread rows shown now and lets go of the others, keeping the
  // clocks of the rows that stay.
  useEffect(() => {
    const now = new Set(list?.querySelectorAll<HTMLElement>("[data-unread]") ?? []);
    for (const el of observed.current) {
      if (now.has(el)) continue;
      observer.current?.unobserve(el);
      observed.current.delete(el);
      since.current.delete(el.dataset.key!);
    }
    for (const el of now) {
      if (observed.current.has(el)) continue;
      observed.current.add(el);
      observer.current?.observe(el);
    }
  }, [list, messages, shown]);
}

function troubled(servers: Server[]) {
  return servers.some((sv) => sv.state === "authFailed" || sv.state === "backoff" || (sv.state === "disconnected" && sv.error));
}

function ServerBanners({ servers, onRelogin }: { servers: Server[]; onRelogin(sv: Server): void }) {
  const now = useNow(1000);
  const t = useT();
  const down = servers.filter((sv) => sv.state === "authFailed" || sv.state === "backoff" || (sv.state === "disconnected" && sv.error));
  if (down.length === 0) return null;
  return (
    <VStack gap={2}>
      {down.map((sv) =>
        sv.state === "authFailed" ? (
          <Banner
            key={sv.id}
            status="error"
            title={t.signInTo(sv.name)}
            description={sv.error || t.sessionEnded}
            endContent={<Button label={t.signIn} size="sm" onClick={() => onRelogin(sv)} />}
          />
        ) : sv.state === "backoff" ? (
          <Banner key={sv.id} status="warning" title={t.cantReach(sv.name)} description={statusText(t, sv, now)} />
        ) : (
          <Banner key={sv.id} status="error" title={t.notConnected(sv.name)} description={sv.error} />
        ),
      )}
    </VStack>
  );
}

function MessageSkeletons() {
  return (
    <div className="msg-skeletons" aria-hidden="true">
      {Array.from({ length: 7 }, (_, i) => (
        <div key={i} className="msg-skel">
          <Skeleton width={32} height={32} radius={2} index={i} />
          <div className="msg-skel-lines">
            <Skeleton width="62%" height={12} radius={2} index={i} />
            <Skeleton width="36%" height={10} radius={2} index={i} />
            <Skeleton width="84%" height={10} radius={2} index={i} />
          </div>
        </div>
      ))}
    </div>
  );
}

export interface MessagesPageProps {
  state: State;
  serverId: number;
  appId: number;
  onRelogin(sv: Server): void;
  onError(msg: string): void;
}

export function MessagesPage({ state, serverId, appId, onRelogin, onError }: MessagesPageProps) {
  const t = useT();
  const [pageEl, setPageEl] = useState<HTMLDivElement | null>(null);
  const [pageWidth, setPageWidth] = useState(0);
  const [search, setSearch] = useState("");
  const [query, setQuery] = useState("");
  const [limit, setLimit] = useState(pageSize);
  const [refresh, setRefresh] = useState(0);
  const [historical, setHistorical] = useState(false);
  const [page, setPage] = useState<MessagePage | null>(null);
  const loaded = useRef<{ scope: string; gen: number; page: MessagePage; anchor: MessageRef | null } | null>(null);
  const [deleting, setDeleting] = useState<ReadonlySet<string>>(new Set());
  const [readNow, setReadNow] = useState<ReadonlySet<string>>(new Set());
  const [selected, setSelected] = useState("");
  const [reading, setReading] = useState(false);
  const [highlight, setHighlight] = useState("");
  const [scrollTo, setScrollTo] = useState("");
  const [focusKey, setFocusKey] = useState("");
  const [scroller, setScroller] = useState<HTMLElement | null>(null);
  const [list, setList] = useState<HTMLElement | null>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const shownQuery = useRef(query);
  const messagesRef = useRef<Message[]>([]);
  const selectedRef = useRef("");
  const readingRef = useRef(false);
  const wideRef = useRef(false);
  const deletingRef = useRef(deleting);
  const nav = useNavTarget();
  const target = nav && nav.serverId !== 0 && (serverId === 0 || nav.serverId === serverId) ? nav : null;

  useLayoutEffect(() => {
    if (!pageEl) return;
    const apply = () => {
      const w = pageEl.clientWidth;
      setPageWidth((prev) => (prev === w ? prev : w));
    };
    apply();
    const ro = new ResizeObserver(apply);
    ro.observe(pageEl);
    return () => ro.disconnect();
  }, [pageEl]);
  const wide = pageWidth >= splitAt;
  wideRef.current = wide;
  readingRef.current = reading;
  selectedRef.current = selected;
  deletingRef.current = deleting;

  // A new search starts again from the first page.
  useEffect(() => {
    const handle = setTimeout(() => {
      if (search === shownQuery.current) return;
      shownQuery.current = search;
      setQuery(search);
      setLimit(pageSize);
      scroller?.scrollTo({ top: 0 });
    }, 200);
    return () => clearTimeout(handle);
  }, [search, scroller]);

  // A notification's message shows whatever the search was.
  useEffect(() => {
    if (!target) return;
    shownQuery.current = "";
    setSearch("");
    setQuery("");
  }, [target]);

  // Refreshes the loaded window when messages change; scrolling requests only
  // the next bounded page. Notification navigation starts at its target.
  useEffect(() => {
    let live = true;
    const include = target && target.messageId !== 0 ? { serverId: target.serverId, id: target.messageId } : null;
    const scope = `${serverId}/${appId}/${target ? "" : query}`;
    const previous = loaded.current;
    const anchor = include ?? (previous?.scope === scope ? previous.anchor : null);
    const load = async () => {
      // Growing an unchanged list reads only its next page. After a mutation,
      // refresh the loaded window using bounded cursor queries.
      const reuse = !target && previous?.scope === scope && previous.gen === state.msgGen;
      let result = reuse ? previous.page : await Desktop.messages({ serverId, appId, search: target ? "" : query, limit: pageSize, include: anchor, before: null });
      const hasNewer = result.hasNewer;
      const all = [...result.messages];
      while (live && result.hasMore && result.next && all.length < limit) {
        result = await Desktop.messages({ serverId, appId, search: target ? "" : query, limit: pageSize, include: null, before: result.next });
        all.push(...result.messages);
      }
      if (!live) return;
      // A message can move into a refreshed page while queries are in flight.
      const unique = [...new Map(all.map((m) => [keyOf(m), m])).values()];
      const p = { ...result, hasNewer, messages: unique };
      // A target that is the newest message leaves the list at the latest, where new messages show.
      const effectiveAnchor = anchor && hasNewer && unique.some((m) => keyOf(m) === keyOf(anchor)) ? anchor : null;
      loaded.current = { scope, gen: state.msgGen, page: p, anchor: effectiveAnchor };
      setHistorical(effectiveAnchor !== null);
      setPage((prev) => ({ ...p, messages: reconcile(prev?.messages ?? [], p.messages) }));
      if (!target) return;
      const key = keyOf({ serverId: target.serverId, id: target.messageId });
      if (target.messageId !== 0 && p.messages.some((m) => keyOf(m) === key)) {
        setScrollTo(key);
        setHighlight(key);
        setSelected(key);
        setReading(true);
        const hit = p.messages.find((m) => keyOf(m) === key);
        if (hit && !hit.read) {
          setReadNow((s) => (s.has(key) ? s : new Set(s).add(key)));
          void Desktop.markRead(hit.serverId, [hit.id]).catch((err) => {
            setReadNow((s) => {
              if (!s.has(key)) return s;
              const n = new Set(s);
              n.delete(key);
              return n;
            });
            onError(errorText(err));
          });
        }
      }
      clearNavTarget();
    };
    void load().catch((err) => live && onError(t.couldNotLoad(errorText(err))));
    return () => {
      live = false;
    };
  }, [serverId, appId, query, limit, state.msgGen, target, onError, t, refresh]);

  const messages = page?.messages ?? [];
  messagesRef.current = messages;
  // Messages that arrive or go above the row read keep it where it is on
  // screen (the virtualizer anchors to a row when anchoring to the end); at
  // the top, new messages show.
  const keepReading = (scroller?.scrollTop ?? 0) > 0;
  const getItemKey = useCallback((i: number) => keyOf(messages[i]!), [messages]);
  const virtualizer = useVirtualizer({
    count: messages.length,
    getScrollElement: () => scroller,
    estimateSize: () => estimatedRowHeight,
    getItemKey,
    overscan: rowOverscan,
    anchorTo: keepReading ? "end" : "start",
  });
  const virtualizerRef = useRef(virtualizer);
  virtualizerRef.current = virtualizer;
  const rows = virtualizer.getVirtualItems();

  useEffect(() => {
    if (!scrollTo) return;
    const i = messages.findIndex((m) => keyOf(m) === scrollTo);
    if (i < 0) {
      if (page) setScrollTo("");
      return;
    }
    if (!list || (!wide && reading)) return;
    virtualizer.scrollToIndex(i, { align: "center" });
    setScrollTo("");
  }, [scrollTo, messages, list, wide, reading, virtualizer, page]);

  useEffect(() => {
    if (!focusKey) return;
    if (page && !messages.some((m) => keyOf(m) === focusKey)) {
      setFocusKey("");
      return;
    }
    if ((!wide && reading) || !list) return;
    const i = messages.findIndex((m) => keyOf(m) === focusKey);
    if (i >= 0) virtualizer.scrollToIndex(i, { align: "auto" });
    const el = list.querySelector<HTMLElement>(`[data-key="${CSS.escape(focusKey)}"]`);
    if (!el) return;
    el.focus({ preventScroll: true });
    setFocusKey("");
  }, [focusKey, messages, list, wide, reading, rows, virtualizer, page]);

  useEffect(() => {
    if (!highlight) return;
    const handle = setTimeout(() => setHighlight(""), 4000);
    return () => clearTimeout(handle);
  }, [highlight]);

  useEffect(() => {
    if (!selected || !page) return;
    if (!page.messages.some((m) => keyOf(m) === selected)) {
      setSelected("");
      setReading(false);
    }
  }, [page, selected]);

  // Loads more when the end of the list comes near.
  const hasMore = page?.hasMore ?? false;
  const count = messages.length;
  const lastShown = rows.at(-1)?.index ?? -1;
  useEffect(() => {
    if (hasMore && lastShown >= count - loadMoreAhead) setLimit(count + pageSize);
  }, [hasMore, lastShown, count]);

  useMarkVisibleRead(list, messages, rows.map((r) => r.key).join(), onError);

  const markReadNow = useCallback(
    (m: Message) => {
      if (m.read) return;
      const k = keyOf(m);
      setReadNow((s) => (s.has(k) ? s : new Set(s).add(k)));
      void Desktop.markRead(m.serverId, [m.id]).catch((err) => {
        setReadNow((s) => {
          if (!s.has(k)) return s;
          const n = new Set(s);
          n.delete(k);
          return n;
        });
        onError(errorText(err));
      });
    },
    [onError],
  );

  const selectMessage = useCallback(
    (m: Message, focus: boolean) => {
      const k = keyOf(m);
      setSelected(k);
      setReading(true);
      markReadNow(m);
      if (focus && wideRef.current) setFocusKey(k);
    },
    [markReadNow],
  );

  const onDelete = useCallback(
    (m: Message) => {
      const k = keyOf(m);
      if (deletingRef.current.has(k)) return;
      const listNow = messagesRef.current;
      const idx = listNow.findIndex((x) => keyOf(x) === k);
      const neighbor = idx >= 0 ? (listNow[idx + 1] ?? listNow[idx - 1]) : undefined;
      const nextKey = neighbor && keyOf(neighbor) !== k ? keyOf(neighbor) : "";
      const wasSelected = selectedRef.current === k;
      // The deleted row, or the reading pane's button, is about to leave the
      // page: the focus would fall to the body.
      const active = document.activeElement;
      const hadFocus = active instanceof Element && active.closest(".msg-list-pane, .msg-read-pane") !== null;
      if (wasSelected) {
        setSelected(nextKey);
        if (!nextKey) setReading(false);
        if (hadFocus && nextKey) setFocusKey(nextKey);
      }
      setDeleting((s) => new Set(s).add(k));
      Desktop.deleteMessage(m.serverId, m.id)
        .catch((err) => {
          if (wasSelected) setSelected((cur) => (cur === nextKey ? k : cur));
          onError(t.couldNotDelete(errorText(err)));
        })
        .finally(() =>
          setDeleting((s) => {
            if (!s.has(k)) return s;
            const n = new Set(s);
            n.delete(k);
            return n;
          }),
        );
    },
    [onError, t],
  );

  const focusSearch = useCallback(() => {
    const input = searchRef.current;
    if (!input) return;
    input.focus();
    input.select();
  }, []);

  const focusList = useCallback(() => {
    setReading(false);
    const msgs = messagesRef.current;
    const current = selectedRef.current;
    const key = current && msgs.some((m) => keyOf(m) === current) ? current : msgs[0] ? keyOf(msgs[0]) : "";
    if (!key) {
      scroller?.focus();
      return;
    }
    setFocusKey(key);
  }, [scroller]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.isComposing) return;
      const targetEl = e.target;
      if (!(targetEl instanceof Element)) return;
      if ((e.ctrlKey || e.metaKey) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === "f") {
        if (targetEl.closest("[role='dialog']")) return;
        e.preventDefault();
        focusSearch();
        return;
      }
      if (targetEl.closest("input, textarea, select, [contenteditable='true']")) return;
      // An open menu or dialog owns Escape and the arrows. Closed popovers stay
      // in the tree, so only a box that is actually showing counts.
      const overlayOpen = [...document.querySelectorAll("[role='menu'], [role='dialog']")].some((el) => {
        if (!(el instanceof HTMLElement)) return false;
        const box = el.getBoundingClientRect();
        return box.width > 0 && box.height > 0 && getComputedStyle(el).visibility !== "hidden";
      });
      if (targetEl.closest("[role='menu'], [role='dialog']") || overlayOpen) return;
      if (e.defaultPrevented) return;
      const inPage = !!pageEl && pageEl.contains(targetEl);
      const bare = targetEl === document.body || targetEl === document.documentElement;
      if (!inPage && !bare) return;

      if (e.key === "Escape") {
        if (!wideRef.current && readingRef.current) {
          e.preventDefault();
          setReading(false);
          const k = selectedRef.current;
          if (k) setFocusKey(k);
          return;
        }
        if (selectedRef.current) {
          e.preventDefault();
          setSelected("");
        }
        return;
      }

      const msgs = messagesRef.current;
      const current = () => msgs.find((m) => keyOf(m) === selectedRef.current);
      if (e.key === "ArrowDown" || e.key === "ArrowUp" || e.key === "Home" || e.key === "End") {
        if (e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return;
        if (msgs.length === 0) return;
        e.preventDefault();
        const i = selectedRef.current ? msgs.findIndex((m) => keyOf(m) === selectedRef.current) : -1;
        let n = 0;
        if (e.key === "Home") n = 0;
        else if (e.key === "End") n = msgs.length - 1;
        else if (e.key === "ArrowDown") n = i < 0 ? 0 : Math.min(msgs.length - 1, i + 1);
        else n = i < 0 ? msgs.length - 1 : Math.max(0, i - 1);
        const m = msgs[n];
        if (m) selectMessage(m, true);
        return;
      }
      const macDelete = isMac && e.metaKey && !e.shiftKey && !e.altKey && !e.ctrlKey && e.key === "Backspace";
      if (e.key === "Delete" || macDelete) {
        if (e.key === "Delete" && (e.altKey || e.ctrlKey || e.metaKey || e.shiftKey)) return;
        const m = current();
        if (!m) return;
        e.preventDefault();
        onDelete(m);
        return;
      }
      if (e.key === "Enter" && !e.shiftKey && !e.altKey && !e.ctrlKey && !e.metaKey) {
        if (targetEl.closest("button, a")) return;
        const m = current();
        if (!m?.clickUrl) return;
        e.preventDefault();
        openMessageLink(m.clickUrl);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [focusSearch, onDelete, pageEl, selectMessage]);

  const onSearchKey = (e: ReactKeyboardEvent<HTMLInputElement>) => {
    if (e.nativeEvent.isComposing) return;
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      if (search) setSearch("");
      else focusList();
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      e.stopPropagation();
      focusList();
    }
  };

  const server = serverId ? findServer(state, serverId) : undefined;
  const scopeApp = server && appId ? server.apps.find((a) => a.id === appId) : undefined;
  const title = scopeApp ? scopeApp.name : server ? server.name : t.allMessages;
  const scopeLine = scopeApp ? server!.name : server ? server.url : t.servers(state.servers.length);
  const unreadCount = scopeApp ? scopeApp.unread : server ? server.unread : state.unread;
  const subtitle = unreadCount > 0 ? `${t.unreadCount(unreadCount)} · ${scopeLine}` : scopeLine;
  const scoped = server ? [server] : state.servers;
  const showList = wide || !reading;
  const showReading = wide || reading;
  const showBanners = historical || troubled(scoped);
  const selectedMsg = messages.find((m) => keyOf(m) === selected);
  const selectedServer = selectedMsg ? findServer(state, selectedMsg.serverId) : undefined;
  const selectedApp = selectedServer?.apps.find((a) => a.id === selectedMsg!.appId);
  const cursor = selected || (messages[0] ? keyOf(messages[0]) : "");
  const cursorShown = rows.some((r) => String(r.key) === cursor);

  return (
    <Layout
      ref={setPageEl}
      className="msg-page"
      padding={0}
      header={
        <LayoutHeader padding={0} hasDivider height="var(--toolbar-height, 48px)" className="titlebar">
          <HStack gap={2} vAlign="center" className="msg-toolbar">
            <MobileNavToggle label={t.openSidebar} />
            {!wide && reading && (
              <Button
                label={t.back}
                size="sm"
                variant="ghost"
                isIconOnly
                icon={<ArrowLeftIcon size={16} />}
                tooltip={t.back}
                className="no-shrink"
                onClick={() => {
                  setReading(false);
                  if (selected) setFocusKey(selected);
                }}
              />
            )}
            <div className="msg-toolbar-text">
              <h1 className="toolbar-title">{title}</h1>
              <div className="toolbar-subtitle">{subtitle}</div>
            </div>
            {unreadCount > 0 && (
              <Button
                label={t.markAllRead}
                size="sm"
                variant="ghost"
                isIconOnly
                icon={<CheckCheckIcon size={16} />}
                tooltip={t.markAllRead}
                className="no-shrink"
                onClick={() => void Desktop.markAllRead(serverId, appId).catch((err) => onError(errorText(err)))}
              />
            )}
            <div className="msg-search">
              <TextInput
                ref={searchRef}
                label={t.searchMessages}
                isLabelHidden
                placeholder={t.search}
                startIcon={SearchIcon}
                value={search}
                onChange={setSearch}
                onKeyDown={onSearchKey}
                hasClear
                size="sm"
                width="100%"
                autoComplete="off"
              />
            </div>
          </HStack>
        </LayoutHeader>
      }
      content={
        <LayoutContent padding={0} isScrollable={false} className="msg-body">
          <div className={"msg-split" + (wide ? " is-wide" : "")}>
            {showList && (
              <div className="msg-list-pane">
                {showBanners && (
                  <div className="msg-banners">
                    <ServerBanners servers={scoped} onRelogin={onRelogin} />
                    {historical && (
                      <Banner
                        status="info"
                        title={t.viewingOlder}
                        endContent={
                          <Button
                            label={t.showLatest}
                            size="sm"
                            onClick={() => {
                              loaded.current = null;
                              setHistorical(false);
                              setLimit(pageSize);
                              setRefresh((n) => n + 1);
                              scroller?.scrollTo({ top: 0 });
                            }}
                          />
                        }
                      />
                    )}
                  </div>
                )}
                <div ref={setScroller} className={"msg-list-scroll" + (page && messages.length === 0 ? " is-empty" : "")} tabIndex={-1}>
                  {page === null ? (
                    <MessageSkeletons />
                  ) : messages.length === 0 ? (
                    <div className="msg-list-empty">
                      {query ? (
                        <EmptyState icon={<SearchXIcon size={40} />} title={t.noMatchTitle(query)} description={t.noMatchText} headingLevel={2} />
                      ) : (
                        <EmptyState icon={<InboxIcon size={40} />} title={t.noMessagesTitle} description={t.noMessagesText} headingLevel={2} />
                      )}
                    </div>
                  ) : (
                    <>
                      <div
                        ref={setList}
                        role="listbox"
                        aria-label={title}
                        tabIndex={cursorShown ? -1 : 0}
                        className="msg-list"
                        style={{ height: virtualizer.getTotalSize() }}>
                        {rows.map((r) => {
                          const m = messages[r.index];
                          if (!m) return null;
                          const sv = findServer(state, m.serverId);
                          const k = keyOf(m);
                          return (
                            <div key={r.key} data-index={r.index} ref={virtualizer.measureElement} className="msg-row" style={{ top: r.start }}>
                              <MessageRow
                                msg={m}
                                app={sv?.apps.find((a) => a.id === m.appId)}
                                serverName={state.servers.length > 1 && serverId === 0 ? sv?.name : undefined}
                                highlighted={highlight === k}
                                deleting={deleting.has(k)}
                                selected={selected === k}
                                unread={!m.read && !readNow.has(k)}
                                tabIndex={k === cursor ? 0 : -1}
                                onSelect={(message) => selectMessage(message, true)}
                                onDelete={onDelete}
                              />
                            </div>
                          );
                        })}
                      </div>
                      {hasMore && (
                        <HStack hAlign="center" padding={4}>
                          <Spinner />
                        </HStack>
                      )}
                    </>
                  )}
                </div>
              </div>
            )}
            {showReading && (
              <div className="msg-read-pane">
                {selectedMsg ? (
                  <MessageView
                    key={keyOf(selectedMsg)}
                    msg={selectedMsg}
                    app={selectedApp}
                    serverName={selectedServer?.name}
                    deleting={deleting.has(keyOf(selectedMsg))}
                    onDelete={onDelete}
                  />
                ) : (
                  <div className="msg-read-empty">
                    <EmptyState icon={<InboxIcon size={40} />} title={t.selectMessage} headingLevel={2} />
                  </div>
                )}
              </div>
            )}
          </div>
        </LayoutContent>
      }
    />
  );
}
