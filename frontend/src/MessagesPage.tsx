import { useAppShellMobile } from "@astryxdesign/core/AppShell";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Heading } from "@astryxdesign/core/Heading";
import { MobileNavToggle } from "@astryxdesign/core/MobileNav";
import { HStack, Layout, LayoutContent, LayoutHeader, VStack } from "@astryxdesign/core/Layout";
import { Spinner } from "@astryxdesign/core/Spinner";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { CheckCheckIcon, InboxIcon, SearchIcon, SearchXIcon } from "lucide-react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import { MessageCard } from "./MessageCard";
import { Desktop, type Message, type MessagePage, type MessageRef, type Server, type State } from "./mygo";
import { useT } from "./i18n";
import { statusText } from "./Sidebar";
import { clearNavTarget, errorText, findServer, useNavTarget, useNow } from "./store";

const pageSize = 100;
const markReadDelay = 1200;
// The list renders the cards on screen and a few around them only, so that
// the page's memory does not grow with the messages it went through.
const cardGap = 12; // --spacing-3
const estimatedCardHeight = 140;
const cardOverscan = 4;
// The next page loads when the cards rendered reach this close to the end.
const loadMoreAhead = 10;

const keyOf = (m: { serverId: number; id: number }) => `${m.serverId}/${m.id}`;

/** Keeps the objects of messages that did not change, so their cards do not render again. */
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
 * Marks an unread message read once its card stayed on screen for a moment in
 * a focused window. A card counts as on screen once any of it reaches the
 * upper three quarters of the window, however tall it is. shown names the
 * cards rendered, which change as the list scrolls.
 */
function useMarkVisibleRead(list: HTMLElement | null, messages: Message[], shown: string, onError: (message: string) => void) {
  const latest = useRef(messages);
  latest.current = messages;
  const observer = useRef<IntersectionObserver | null>(null);
  // When each card on screen came into view, while the window had the focus.
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
  // Observes the unread cards shown now and lets go of the others, keeping the
  // clocks of the cards that stay.
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

function ServerBanners({ servers, onRelogin }: { servers: Server[]; onRelogin(sv: Server): void }) {
  const now = useNow(1000);
  const t = useT();
  const troubled = servers.filter((sv) => sv.state === "authFailed" || sv.state === "backoff" || (sv.state === "disconnected" && sv.error));
  if (troubled.length === 0) return null;
  return (
    <VStack gap={2}>
      {troubled.map((sv) =>
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

export interface MessagesPageProps {
  state: State;
  serverId: number;
  appId: number;
  onRelogin(sv: Server): void;
  onError(msg: string): void;
}

export function MessagesPage({ state, serverId, appId, onRelogin, onError }: MessagesPageProps) {
  const t = useT();
  const { isMobile } = useAppShellMobile();
  const [search, setSearch] = useState("");
  const [query, setQuery] = useState("");
  const [limit, setLimit] = useState(pageSize);
  const [refresh, setRefresh] = useState(0);
  const [historical, setHistorical] = useState(false);
  const [page, setPage] = useState<MessagePage | null>(null);
  const loaded = useRef<{ scope: string; gen: number; page: MessagePage; anchor: MessageRef | null } | null>(null);
  const [deleting, setDeleting] = useState<ReadonlySet<string>>(new Set());
  const [highlight, setHighlight] = useState("");
  const [scrollTo, setScrollTo] = useState("");
  const [scroller, setScroller] = useState<HTMLElement | null>(null);
  const [list, setList] = useState<HTMLElement | null>(null);
  const [listTop, setListTop] = useState(0);
  const shownQuery = useRef(query);
  const nav = useNavTarget();
  const target = nav && nav.serverId !== 0 && (serverId === 0 || nav.serverId === serverId) ? nav : null;

  // A new search starts again from the first page.
  useEffect(() => {
    const t = setTimeout(() => {
      if (search === shownQuery.current) return;
      shownQuery.current = search;
      setQuery(search);
      setLimit(pageSize);
      scroller?.scrollTo({ top: 0 });
    }, 200);
    return () => clearTimeout(t);
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
      }
      clearNavTarget();
    };
    void load().catch((err) => live && onError(t.couldNotLoad(errorText(err))));
    return () => {
      live = false;
    };
  }, [serverId, appId, query, limit, state.msgGen, target, onError, t, refresh]);

  const messages = page?.messages ?? [];
  // Where the list starts in the scrolled content, below the banners.
  useLayoutEffect(() => {
    if (!list || !scroller) return;
    const top = list.getBoundingClientRect().top - scroller.getBoundingClientRect().top + scroller.scrollTop;
    if (top !== listTop) setListTop(top);
  });
  // Messages that arrive or go above the card read keep it where it is on
  // screen, as the browser did for cards in the flow (the virtualizer anchors
  // to a card when anchoring to the end); at the top, new messages show.
  const keepReading = (scroller?.scrollTop ?? 0) > 0;
  const getItemKey = useCallback((i: number) => keyOf(messages[i]!), [messages]);
  const virtualizer = useVirtualizer({
    count: messages.length,
    getScrollElement: () => scroller,
    estimateSize: () => estimatedCardHeight,
    getItemKey,
    overscan: cardOverscan,
    gap: cardGap,
    scrollMargin: listTop,
    anchorTo: keepReading ? "end" : "start",
  });
  const rows = virtualizer.getVirtualItems();

  useEffect(() => {
    const i = scrollTo ? messages.findIndex((m) => keyOf(m) === scrollTo) : -1;
    if (i < 0 || !list) return;
    virtualizer.scrollToIndex(i, { align: "center" });
    setScrollTo("");
  }, [scrollTo, messages, list, virtualizer]);
  useEffect(() => {
    if (!highlight) return;
    const t = setTimeout(() => setHighlight(""), 4000);
    return () => clearTimeout(t);
  }, [highlight]);

  // Loads more when the end of the list comes near.
  const hasMore = page?.hasMore ?? false;
  const count = messages.length;
  const lastShown = rows.at(-1)?.index ?? -1;
  useEffect(() => {
    if (hasMore && lastShown >= count - loadMoreAhead) setLimit(count + pageSize);
  }, [hasMore, lastShown, count]);

  useMarkVisibleRead(list, messages, rows.map((r) => r.key).join(), onError);

  const onDelete = useCallback(
    (m: Message) => {
      const k = keyOf(m);
      setDeleting((s) => new Set(s).add(k));
      Desktop.deleteMessage(m.serverId, m.id)
        .catch((err) => onError(t.couldNotDelete(errorText(err))))
        .finally(() =>
          setDeleting((s) => {
            const n = new Set(s);
            n.delete(k);
            return n;
          }),
        );
    },
    [onError, t],
  );

  const server = serverId ? findServer(state, serverId) : undefined;
  const app = server && appId ? server.apps.find((a) => a.id === appId) : undefined;
  const title = app ? app.name : server ? server.name : t.allMessages;
  const subtitle = app ? server!.name : server ? server.url : t.servers(state.servers.length);
  const unread = app ? app.unread : server ? server.unread : state.unread;
  const scoped = server ? [server] : state.servers;

  return (
    <Layout
      contentWidth={860}
      padding={5}
      header={
        <LayoutHeader hasDivider className="titlebar">
          <HStack gap={3} vAlign="center">
            <MobileNavToggle label={t.openSidebar} />
            <VStack gap={0.5} className="page-title">
              <Heading level={1} maxLines={1}>
                {title}
              </Heading>
              <Text type="supporting" maxLines={1}>
                {unread > 0 ? `${t.unreadCount(unread)} · ${subtitle}` : subtitle}
              </Text>
            </VStack>
            {unread > 0 && (
              <Button
                label={t.markAllRead}
                icon={<CheckCheckIcon size={16} />}
                isIconOnly={isMobile}
                tooltip={isMobile ? t.markAllRead : undefined}
                className="no-shrink"
                onClick={() => void Desktop.markAllRead(serverId, appId).catch((err) => onError(errorText(err)))}
              />
            )}
            <TextInput
              label={t.searchMessages}
              isLabelHidden
              placeholder={t.search}
              startIcon={SearchIcon}
              value={search}
              onChange={setSearch}
              hasClear
              width={isMobile ? 160 : 240}
            />
          </HStack>
        </LayoutHeader>
      }
      content={
        <LayoutContent ref={setScroller}>
          <VStack gap={3}>
            <ServerBanners servers={scoped} onRelogin={onRelogin} />
            {historical && (
              <Banner status="info" title={t.viewingOlder} endContent={<Button label={t.showLatest} size="sm" onClick={() => {
                loaded.current = null;
                setHistorical(false);
                setLimit(pageSize);
                setRefresh((n) => n + 1);
                scroller?.scrollTo({ top: 0 });
              }} />} />
            )}
            {page === null ? (
              <HStack hAlign="center" padding={10}>
                <Spinner size="lg" />
              </HStack>
            ) : page.messages.length === 0 ? (
              query ? (
                <EmptyState icon={<SearchXIcon size={40} />} title={t.noMatchTitle(query)} description={t.noMatchText} />
              ) : (
                <EmptyState icon={<InboxIcon size={40} />} title={t.noMessagesTitle} description={t.noMessagesText} />
              )
            ) : (
              <div ref={setList} className="msg-list" style={{ height: virtualizer.getTotalSize() }}>
                {rows.map((r) => {
                  const m = messages[r.index]!;
                  const sv = findServer(state, m.serverId);
                  const k = keyOf(m);
                  return (
                    <div
                      key={r.key}
                      data-index={r.index}
                      ref={virtualizer.measureElement}
                      className="msg-row"
                      style={{ transform: `translateY(${r.start - listTop}px)` }}>
                      <MessageCard
                        msg={m}
                        app={sv?.apps.find((a) => a.id === m.appId)}
                        serverName={state.servers.length > 1 && serverId === 0 ? sv?.name : undefined}
                        highlighted={highlight === k}
                        deleting={deleting.has(k)}
                        onDelete={onDelete}
                      />
                    </div>
                  );
                })}
              </div>
            )}
            {hasMore && (
              <HStack hAlign="center" padding={4}>
                <Spinner />
              </HStack>
            )}
          </VStack>
        </LayoutContent>
      }
    />
  );
}
