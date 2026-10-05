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
import { useCallback, useEffect, useRef, useState } from "react";
import { MessageCard } from "./MessageCard";
import { Desktop, type Message, type MessagePage, type Server, type State } from "./mygo";
import { useT } from "./i18n";
import { statusText } from "./Sidebar";
import { clearNavTarget, errorText, findServer, useNavTarget, useNow } from "./store";

const pageSize = 100;
const markReadDelay = 1200;

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
 * upper three quarters of the window, however tall it is.
 */
function useMarkVisibleRead(list: HTMLElement | null, messages: Message[]) {
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
      for (const [server, ids] of byServer) void Desktop.markRead(server, ids);
    }, 200);
    return () => {
      io.disconnect();
      observer.current = null;
      seen.clear();
      clearInterval(tick);
      removeEventListener("focus", restart);
      document.removeEventListener("visibilitychange", restart);
    };
  }, [list, height]);
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
  }, [list, messages]);
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
  const [page, setPage] = useState<MessagePage | null>(null);
  const [deleting, setDeleting] = useState<ReadonlySet<string>>(new Set());
  const [highlight, setHighlight] = useState("");
  const [scrollTo, setScrollTo] = useState("");
  const [list, setList] = useState<HTMLElement | null>(null);
  const sentinel = useRef<HTMLDivElement>(null);
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
      list?.scrollIntoView({ block: "start" });
    }, 200);
    return () => clearTimeout(t);
  }, [search, list]);

  // A notification's message shows whatever the search was.
  useEffect(() => {
    if (!target) return;
    shownQuery.current = "";
    setSearch("");
    setQuery("");
  }, [target]);

  // Loads the page again whenever its query or the messages change. With a
  // notification's message to show, the page grows to hold it and keeps that size.
  useEffect(() => {
    let live = true;
    const include = target && target.messageId !== 0 ? { serverId: target.serverId, id: target.messageId } : null;
    Desktop.messages({ serverId, appId, search: target ? "" : query, limit, include }).then(
      (p) => {
        if (!live) return;
        setPage((prev) => ({ ...p, messages: reconcile(prev?.messages ?? [], p.messages) }));
        if (!target) return;
        const key = keyOf({ serverId: target.serverId, id: target.messageId });
        if (target.messageId !== 0 && p.messages.some((m) => keyOf(m) === key)) {
          setLimit((l) => Math.max(l, p.messages.length));
          setScrollTo(key);
          setHighlight(key);
        }
        clearNavTarget();
      },
      (err) => live && onError(t.couldNotLoad(errorText(err))),
    );
    return () => {
      live = false;
    };
  }, [serverId, appId, query, limit, state.msgGen, target, onError, t]);

  useEffect(() => {
    if (!scrollTo || !list || !page?.messages.some((m) => keyOf(m) === scrollTo)) return;
    list.querySelector(`[data-key="${scrollTo}"]`)?.scrollIntoView({ block: "center" });
    setScrollTo("");
  }, [scrollTo, page, list]);
  useEffect(() => {
    if (!highlight) return;
    const t = setTimeout(() => setHighlight(""), 4000);
    return () => clearTimeout(t);
  }, [highlight]);

  // Loads more when the end of the list shows.
  const hasMore = page?.hasMore ?? false;
  const count = page?.messages.length ?? 0;
  useEffect(() => {
    const el = sentinel.current;
    if (!el || !hasMore) return;
    const io = new IntersectionObserver((e) => e[0]?.isIntersecting && setLimit(count + pageSize), { rootMargin: "600px" });
    io.observe(el);
    return () => io.disconnect();
  }, [hasMore, count]);

  useMarkVisibleRead(list, page?.messages ?? []);

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
                onClick={() => void Desktop.markAllRead(serverId, appId)}
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
        <LayoutContent>
          <VStack gap={3} ref={setList}>
            <ServerBanners servers={scoped} onRelogin={onRelogin} />
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
              page.messages.map((m) => {
                const sv = findServer(state, m.serverId);
                const k = keyOf(m);
                return (
                  <MessageCard
                    key={k}
                    msg={m}
                    app={sv?.apps.find((a) => a.id === m.appId)}
                    serverName={state.servers.length > 1 && serverId === 0 ? sv?.name : undefined}
                    highlighted={highlight === k}
                    deleting={deleting.has(k)}
                    onDelete={onDelete}
                  />
                );
              })
            )}
            <div ref={sentinel} />
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
