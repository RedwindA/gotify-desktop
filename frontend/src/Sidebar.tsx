import { useAppShellMobile } from "@astryxdesign/core/AppShell";
import { Avatar } from "@astryxdesign/core/Avatar";
import { Badge } from "@astryxdesign/core/Badge";
import { Icon } from "@astryxdesign/core/Icon";
import { MoreMenu } from "@astryxdesign/core/MoreMenu";
import { MobileNav } from "@astryxdesign/core/MobileNav";
import { SideNav, SideNavItem, SideNavSection } from "@astryxdesign/core/SideNav";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { BellIcon, BellOffIcon, InboxIcon, PlusIcon, SettingsIcon } from "lucide-react";
import type { App, Server, State } from "./mygo";
import { useT, type Messages } from "./i18n";
import { go as goTo, useAppImage, useNow, type Route } from "./store";

export interface ServerActions {
  edit(sv: Server): void;
  relogin(sv: Server): void;
  remove(sv: Server): void;
  markAllRead(serverId: number, appId: number): void;
  appPrefs(sv: Server, app: App): void;
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
                  { label: t.removeServer, variant: "destructive", onClick: () => actions.remove(sv) },
                ]}
              />
            }
          />
          {sv.apps.map((a) => (
            <SideNavItem
              key={a.id}
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
                  ]}
                />
              }
            />
          ))}
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
