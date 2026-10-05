import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { Layout, LayoutContent, VStack } from "@astryxdesign/core/Layout";
import { Selector } from "@astryxdesign/core/Selector";
import { Switch } from "@astryxdesign/core/Switch";
import { Desktop, type App, type Server } from "./mygo";
import { useT } from "./i18n";
import { errorText } from "./store";

/** Edits the notification preferences of an app; changes apply at once. */
export function AppPrefsDialog({ server, app, onClose, onError }: { server: Server; app: App; onClose(): void; onError(msg: string): void }) {
  const t = useT();
  const priorities = [{ value: "0", label: t.everyMessage }, ...[1, 4, 8].map((n) => ({ value: String(n), label: t.priorityAndUp(n) }))];
  const save = (muted: boolean, minPriority: number | null) =>
    Desktop.setAppPrefs(server.id, app.id, { muted, minPriority }).catch((err) => onError(t.couldNotSave(errorText(err))));
  return (
    <Dialog isOpen onOpenChange={(open) => !open && onClose()} width={420}>
      <Layout
        header={<DialogHeader title={app.name} subtitle={t.appPrefsSubtitle(server.name)} onOpenChange={() => onClose()} />}
        content={
          <LayoutContent>
            <VStack gap={5}>
              <Switch
                label={t.mute}
                description={t.muteText}
                value={app.muted}
                labelPosition="start"
                labelSpacing="spread"
                width="100%"
                changeAction={(on) => save(on, app.minPriority)}
              />
              <Selector
                label={t.notifyAbout}
                options={priorities}
                value={String(app.minPriority ?? 0)}
                isDisabled={app.muted}
                width="100%"
                onChange={(v) => void save(app.muted, v === "0" ? null : Number(v))}
              />
            </VStack>
          </LayoutContent>
        }
      />
    </Dialog>
  );
}
