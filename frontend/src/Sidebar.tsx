import { useAppShellMobile } from "@astryxdesign/core/AppShell";
import { Avatar } from "@astryxdesign/core/Avatar";
import { Badge } from "@astryxdesign/core/Badge";
import { Icon } from "@astryxdesign/core/Icon";
import { MoreMenu } from "@astryxdesign/core/MoreMenu";
import { MobileNav } from "@astryxdesign/core/MobileNav";
import { SideNav, SideNavItem, SideNavSection } from "@astryxdesign/core/SideNav";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { DndContext, MouseSensor, TouchSensor, useSensor, useSensors, type DragEndEvent } from "@dnd-kit/core";
import { restrictToVerticalAxis } from "@dnd-kit/modifiers";
import { SortableContext, arrayMove, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { BellIcon, BellOffIcon, InboxIcon, PlusIcon, SettingsIcon } from "lucide-react";
import { useState, type ReactNode } from "react";
import type { App, Server, State } from "./mygo";
import { useT, type Messages } from "./i18n";
import { go as goTo, useAppImage, useNow, type Route } from "./store";

export interface ServerActions {
  edit(sv: Server): void;
  relogin(sv: Server): void;
  remove(sv: Server): void;
  markAllRead(serverId: number, appId: number): void;
  /** Deletes the messages of an app, or of the server without one. */
  clear(sv: Server, app?: App): void;
  appPrefs(sv: Server, app: App): void;
  /** Moves an app before another, or last when beforeId is 0, and settles once the state shows it. */
  moveApp(sv: Server, appId: number, beforeId: number): Promise<void>;
}

function count(n: number) {
  return n > 0 ? <Badge label={n > 999 ? "999+" : n} /> : undefined;
}

export function statusText(t: Messages, sv: Server, now: number): string {
  switch (sv.state) {
    case "connected":
      return t.connected;
    case "connecting":
      return t.connecting;
    case "backoff": {
      const wait = sv.retryAt ? Math.max(0, Math.round((Date.parse(sv.retryAt) - now) / 1000)) : 0;
      return t.retryingIn(wait) + (sv.error ? ` — ${sv.error}` : "");
    }
    case "authFailed":
      return t.signInAgainStatus + (sv.error ? ` — ${sv.error}` : "");
  }
  return sv.error || t.disconnected;
}

const dotVariant = { connected: "success", connecting: "warning", backoff: "warning", authFailed: "error", disconnected: "neutral" } as const;

function ServerStatus({ sv }: { sv: Server }) {
  const now = useNow(1000);
  const t = useT();
  const text = statusText(t, sv, now);
  return <StatusDot variant={dotVariant[sv.state]} label={text} tooltip={text} isPulsing={sv.state === "connecting"} />;
}

export function AppAvatar({ serverId, app, size }: { serverId: number; app: App | undefined; size: "xsm" | "sm" | "md" }) {
  const src = useAppImage(serverId, app?.id ?? 0, app?.imageKey ?? "");
  return <Avatar size={size} shape="rounded" name={app?.name || "?"} src={src} tooltip={false} />;
}

/** An app's row, which the pointer drags to reorder the apps of its server. */
function SortableApp({ id, disabled, children }: { id: number; disabled: boolean; children: ReactNode }) {
  const { setNodeRef, listeners, transform, transition, isDragging } = useSortable({ id, disabled });
  return (
    <div
      ref={setNodeRef}
      className={isDragging ? "sidenav-app dragging" : "sidenav-app"}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      {...listeners}
    >
      {children}
    </div>
  );
}

/** The apps of a server in their order; the pointer drags them, the menu moves them up and down. */
function ServerApps({
  sv,
  actions,
  isSel,
  go,
}: {
  sv: Server;
  actions: ServerActions;
  isSel(serverId: number, appId: number): boolean;
  go(r: Route): void;
}) {
  const t = useT();
  // A moved app keeps its new place until the call settles, so it does not
  // jump back while the server answers.
  const [moving, setMoving] = useState<number[] | null>(null);
  // A small distance tells a drag from a click. A finger first holds still, so
  // that a swipe still scrolls the sidebar.
  const sensors = useSensors(
    useSensor(MouseSensor, { activationConstraint: { distance: 5 } }),
    useSensor(TouchSensor, { activationConstraint: { delay: 250, tolerance: 5 } }),
  );
  const byId = new Map(sv.apps.map((a) => [a.id, a]));
  const apps = moving && moving.length === sv.apps.length && moving.every((id) => byId.has(id)) ? moving.map((id) => byId.get(id)!) : sv.apps;
  const ids = apps.map((a) => a.id);
  const move = (from: number, to: number) => {
    if (from === to || to < 0 || to >= ids.length) return;
    const next = arrayMove(ids, from, to);
    setMoving(next);
    void actions.moveApp(sv, ids[from]!, next[to + 1] ?? 0).finally(() => setMoving(null));
  };
  const onDragEnd = ({ active, over }: DragEndEvent) => {
    if (over) move(ids.indexOf(Number(active.id)), ids.indexOf(Number(over.id)));
  };
  const sortable = sv.appsSortable && moving === null;
  return (
    <DndContext sensors={sensors} modifiers={[restrictToVerticalAxis]} onDragEnd={onDragEnd}>
      <SortableContext items={ids} strategy={verticalListSortingStrategy} disabled={!sortable}>
        {apps.map((a, i) => (
          <SortableApp key={a.id} id={a.id} disabled={!sortable}>
            <SideNavItem
              label={a.name}
              icon={<AppAvatar serverId={sv.id} app={a} size="xsm" />}
              isSelected={isSel(sv.id, a.id)}
              onClick={() => go({ page: "messages", serverId: sv.id, appId: a.id })}
              endContent={a.muted ? <Icon icon={BellOffIcon} size="sm" color="secondary" label={t.muted} /> : count(a.unread)}
              actions={
                <MoreMenu
                  size="sm"
                  label={t.appOptions(a.name)}
                  items={[
                    { label: t.notificationSettings, onClick: () => actions.appPrefs(sv, a) },
                    { label: t.markAllRead, onClick: () => actions.markAllRead(sv.id, a.id), isDisabled: a.unread === 0 },
                    ...(sv.appsSortable
                      ? [
                          { type: "divider" as const },
                          { label: t.moveUp, onClick: () => move(i, i - 1), isDisabled: !sortable || i === 0 },
                          { label: t.moveDown, onClick: () => move(i, i + 1), isDisabled: !sortable || i === apps.length - 1 },
                        ]
                      : []),
                    { type: "divider" },
                    { label: t.clearMessages, variant: "destructive", onClick: () => actions.clear(sv, a) },
                  ]}
                />
              }
            />
          </SortableApp>
        ))}
      </SortableContext>
    </DndContext>
  );
}

function SidebarTop() {
  const t = useT();
  return (
    <div className="sidenav-top drag">
      <span className="sidenav-brand">
        <BellIcon size={16} aria-hidden="true" />
        <span>{t.appName}</span>
      </span>
    </div>
  );
}

export function Sidebar({
  state,
  route,
  actions,
  onAdd,
  drawer = false,
}: {
  state: State | null;
  route: Route;
  actions: ServerActions;
  onAdd(): void;
  drawer?: boolean;
}) {
  const t = useT();
  // In a narrow window the sidebar is a drawer, which closes once a page is picked.
  const { closeMobileNav } = useAppShellMobile();
  const header = <SidebarTop />;
  if (!state) {
    if (drawer)
      return (
        <MobileNav side="start" header={header}>
          {null}
        </MobileNav>
      );
    return <SideNav header={header}>{null}</SideNav>;
  }
  const go = (r: Route) => {
    goTo(r);
    closeMobileNav();
  };
  const isSel = (serverId: number, appId: number) =>
    route.page === "messages" && route.serverId === serverId && route.appId === appId;
  const footer = (
    <SideNavSection title={t.appName} isHeaderHidden>
      <SideNavItem
        label={t.addServer}
        icon={PlusIcon}
        onClick={() => {
          closeMobileNav();
          onAdd();
        }}
      />
      <SideNavItem label={t.settings} icon={SettingsIcon} isSelected={route.page === "settings"} onClick={() => go({ page: "settings" })} />
    </SideNavSection>
  );
  const body = (
    <>
      <SideNavSection title={t.allMessages} isHeaderHidden>
        <SideNavItem
          label={t.allMessages}
          icon={InboxIcon}
          isSelected={isSel(0, 0)}
          onClick={() => go({ page: "messages", serverId: 0, appId: 0 })}
          endContent={count(state.unread)}
        />
      </SideNavSection>
      {state.servers.map((sv) => (
        <SideNavSection key={sv.id} title={sv.name} endContent={<ServerStatus sv={sv} />}>
          <SideNavItem
            label={t.allFromServer}
            icon={InboxIcon}
            isSelected={isSel(sv.id, 0)}
            onClick={() => go({ page: "messages", serverId: sv.id, appId: 0 })}
            endContent={count(sv.unread)}
            actions={
              <MoreMenu
                size="sm"
                label={t.serverOptions(sv.name)}
                items={[
                  { label: t.editServer, onClick: () => actions.edit(sv) },
                  { label: t.signInAgainMenu, onClick: () => actions.relogin(sv) },
                  { label: t.markAllRead, onClick: () => actions.markAllRead(sv.id, 0), isDisabled: sv.unread === 0 },
                  { type: "divider" },
                  { label: t.clearMessages, variant: "destructive", onClick: () => actions.clear(sv) },
                  { label: t.removeServer, variant: "destructive", onClick: () => actions.remove(sv) },
                ]}
              />
            }
          />
          <ServerApps sv={sv} actions={actions} isSel={isSel} go={go} />
        </SideNavSection>
      ))}
    </>
  );
  // The drawer of a narrow window comes in from the left, where its toggle is;
  // the default ("auto") slides in from the right the first time.
  if (drawer)
    return (
      <MobileNav side="start" header={header}>
        {body}
        {footer}
      </MobileNav>
    );
  return (
    <SideNav header={header} footer={footer}>
      {body}
    </SideNav>
  );
}
