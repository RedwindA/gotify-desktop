import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { AppShell } from "@astryxdesign/core/AppShell";
import { Button } from "@astryxdesign/core/Button";
import { Center } from "@astryxdesign/core/Center";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { MobileNavToggle } from "@astryxdesign/core/MobileNav";
import { Spinner } from "@astryxdesign/core/Spinner";
import { useToast } from "@astryxdesign/core/Toast";
import { BellRingIcon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { AppPrefsDialog } from "./AppPrefsDialog";
import { MessagesPage } from "./MessagesPage";
import { Desktop, type App as GotifyApp, type Server } from "./mygo";
import { ServerDialog, type ServerDialogMode } from "./ServerDialog";
import { SettingsPage } from "./SettingsPage";
import { Sidebar, type ServerActions } from "./Sidebar";
import { useT } from "./i18n";
import { errorText, findServer, go, refreshState, useAppState, useRoute } from "./store";

function Welcome({ onAdd }: { onAdd(): void }) {
  const t = useT();
  return (
    <Center height="100%" className="drag welcome">
      <div className="corner-toggle">
        <MobileNavToggle label={t.openSidebar} />
      </div>
      <EmptyState
        icon={<BellRingIcon size={48} />}
        title={t.welcomeTitle}
        description={t.welcomeText}
        actions={<Button label={t.addServer} variant="primary" onClick={onAdd} />}
      />
    </Center>
  );
}

export function App() {
  const state = useAppState();
  const route = useRoute();
  const toast = useToast();
  const t = useT();
  const [dialog, setDialog] = useState<ServerDialogMode | null>(null);
  const [removing, setRemoving] = useState<Server | null>(null);
  const [removeBusy, setRemoveBusy] = useState(false);
  const [prefs, setPrefs] = useState<{ serverId: number; appId: number } | null>(null);

  const onError = useCallback((body: string) => void toast({ body, type: "error" }), [toast]);
  const onInfo = useCallback((body: string) => void toast({ body, autoHideDuration: 2000 }), [toast]);

  // A server or app that went away leaves its page for all messages.
  useEffect(() => {
    if (!state || route.page !== "messages" || route.serverId === 0) return;
    const sv = findServer(state, route.serverId);
    if (!sv) go({ page: "messages", serverId: 0, appId: 0 });
    else if (route.appId !== 0 && !sv.apps.some((a) => a.id === route.appId)) go({ page: "messages", serverId: sv.id, appId: 0 });
  }, [state, route]);

  if (!state) {
    return (
      <Center height="100vh">
        <Spinner size="xl" />
      </Center>
    );
  }

  const actions: ServerActions = {
    edit: (server) => setDialog({ kind: "edit", server }),
    relogin: (server) => setDialog({ kind: "relogin", server }),
    remove: setRemoving,
    markAllRead: (serverId, appId) => void Desktop.markAllRead(serverId, appId),
    appPrefs: (sv: Server, a: GotifyApp) => setPrefs({ serverId: sv.id, appId: a.id }),
  };
  const openAdd = () => setDialog({ kind: "add" });
  const prefsServer = prefs ? findServer(state, prefs.serverId) : undefined;
  const prefsApp = prefsServer?.apps.find((a) => a.id === prefs?.appId);

  let page;
  if (route.page === "settings") page = <SettingsPage state={state} onError={onError} onInfo={onInfo} />;
  else if (state.servers.length === 0) page = <Welcome onAdd={openAdd} />;
  else
    page = (
      <MessagesPage
        key={`${route.serverId}/${route.appId}`}
        state={state}
        serverId={route.serverId}
        appId={route.appId}
        onRelogin={actions.relogin}
        onError={onError}
      />
    );

  return (
    <>
      <AppShell
        sideNav={<Sidebar state={state} route={route} actions={actions} onAdd={openAdd} />}
        mobileNav={{ hasToggle: false, content: <Sidebar state={state} route={route} actions={actions} onAdd={openAdd} drawer /> }}>
        {page}
      </AppShell>
      {dialog && (
        <ServerDialog
          mode={dialog}
          onClose={() => setDialog(null)}
          onDone={async (id) => {
            if (dialog.kind === "add") {
              // Select the server once the state has it, or the page would fall back to all messages.
              await refreshState().catch(() => {});
              go({ page: "messages", serverId: id, appId: 0 });
              onInfo(t.serverAdded);
            } else if (dialog.kind === "relogin") onInfo(t.signedIn);
            setDialog(null);
          }}
        />
      )}
      {prefsServer && prefsApp && <AppPrefsDialog server={prefsServer} app={prefsApp} onClose={() => setPrefs(null)} onError={onError} />}
      <AlertDialog
        isOpen={removing !== null}
        onOpenChange={(open) => !open && !removeBusy && setRemoving(null)}
        title={t.removeTitle(removing?.name ?? "")}
        description={t.removeText}
        actionLabel={t.removeAction}
        cancelLabel={t.cancel}
        isActionLoading={removeBusy}
        onAction={async () => {
          if (!removing) return;
          setRemoveBusy(true);
          try {
            await Desktop.removeServer(removing.id);
          } catch (err) {
            onError(t.couldNotRemove(errorText(err)));
          } finally {
            setRemoveBusy(false);
            setRemoving(null);
          }
        }}
      />
    </>
  );
}
