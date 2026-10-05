import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { CheckboxInput } from "@astryxdesign/core/CheckboxInput";
import { Collapsible } from "@astryxdesign/core/Collapsible";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { HStack, Layout, LayoutContent, LayoutFooter, VStack } from "@astryxdesign/core/Layout";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useState } from "react";
import { Desktop, type Server } from "./mygo";
import { useT, type Messages } from "./i18n";
import { errorText } from "./store";

export type ServerDialogMode = { kind: "add" } | { kind: "edit"; server: Server } | { kind: "relogin"; server: Server };

const titles = (t: Messages): Record<ServerDialogMode["kind"], [title: string, subtitle: string, action: string]> => ({
  add: [t.addServerTitle, t.addServerSubtitle, t.connect],
  edit: [t.editServerTitle, "", t.save],
  relogin: [t.signInAgainTitle, "", t.signIn],
});

/** Adds a server, edits one or signs in to one again. onDone gets the ID of a new server. */
export function ServerDialog({ mode, onClose, onDone }: { mode: ServerDialogMode; onClose(): void; onDone(id: number): void | Promise<void> }) {
  const t = useT();
  const sv = mode.kind === "add" ? undefined : mode.server;
  const [name, setName] = useState(sv?.name ?? "");
  const [url, setUrl] = useState(sv?.url ?? "");
  const [user, setUser] = useState("");
  const [pass, setPass] = useState("");
  const [insecure, setInsecure] = useState(sv?.insecure ?? false);
  const [caName, setCAName] = useState(sv?.hasCA ? t.customCA : "");
  const [caPem, setCAPem] = useState("");
  const [caChanged, setCAChanged] = useState(false);
  const [caError, setCAError] = useState("");
  const [submitted, setSubmitted] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const [title, sub, action] = titles(t)[mode.kind];
  const subtitle = mode.kind === "relogin" ? t.sessionEndedOn(sv!.name) : sub;
  const needsLogin = mode.kind !== "edit";
  const urlError = mode.kind === "add" && submitted && !url.trim() ? t.enterAddress : undefined;
  const userError = needsLogin && submitted && !user ? t.enterUsername : undefined;
  const passError = needsLogin && submitted && !pass ? t.enterPassword : undefined;

  async function submit() {
    setSubmitted(true);
    if ((mode.kind === "add" && !url.trim()) || (needsLogin && (!user || !pass)) || busy) return;
    setBusy(true);
    setError("");
    try {
      const form = { name, url, user, pass, insecure, ca: caPem, keepCA: !caChanged };
      if (mode.kind === "add") await onDone(await Desktop.addServer(form));
      else if (mode.kind === "edit") {
        await Desktop.updateServer(mode.server.id, form);
        await onDone(mode.server.id);
      } else {
        await Desktop.relogin(mode.server.id, user, pass);
        await onDone(mode.server.id);
      }
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  }

  async function chooseCA() {
    setCAError("");
    try {
      const f = await Desktop.pickCA();
      if (f) {
        setCAName(f.name);
        setCAPem(f.pem);
        setCAChanged(true);
      }
    } catch (err) {
      setCAError(errorText(err));
    }
  }

  return (
    <Dialog isOpen onOpenChange={(open) => !open && onClose()} purpose="form" width={480}>
      <Layout
        header={<DialogHeader title={title} subtitle={subtitle || undefined} onOpenChange={() => onClose()} />}
        content={
          <LayoutContent>
            <VStack gap={4}>
              {error && <Banner status="error" title={error} />}
              {mode.kind !== "relogin" && (
                <TextInput
                  label={t.serverAddress}
                  value={url}
                  onChange={setUrl}
                  placeholder="https://gotify.example.com"
                  isReadOnly={mode.kind === "edit"}
                  hasAutoFocus={mode.kind === "add"}
                  onEnter={submit}
                  status={urlError ? { type: "error", message: urlError } : undefined}
                />
              )}
              {mode.kind !== "relogin" && (
                <TextInput label={t.name} isOptional value={name} onChange={setName} placeholder={t.namePlaceholder} onEnter={submit} />
              )}
              {needsLogin && (
                <TextInput
                  label={t.username}
                  value={user}
                  onChange={setUser}
                  autoComplete="username"
                  hasAutoFocus={mode.kind === "relogin"}
                  onEnter={submit}
                  status={userError ? { type: "error", message: userError } : undefined}
                />
              )}
              {needsLogin && (
                <TextInput
                  label={t.password}
                  type="password"
                  value={pass}
                  onChange={setPass}
                  autoComplete="current-password"
                  onEnter={submit}
                  status={passError ? { type: "error", message: passError } : undefined}
                />
              )}
              {mode.kind !== "relogin" && (
                <Collapsible trigger={<Text weight="medium">{t.advanced}</Text>} defaultIsOpen={insecure || caName !== ""}>
                  <VStack gap={3} paddingBlockStart={2}>
                    <CheckboxInput
                      label={t.skipTLS}
                      description={t.skipTLSText}
                      value={insecure}
                      onChange={setInsecure}
                      status={insecure ? { type: "warning", message: t.skipTLSWarning } : undefined}
                    />
                    <HStack gap={3} vAlign="center">
                      <VStack gap={0.5} width="100%">
                        <Text weight="medium">{t.caCertificate}</Text>
                        <Text type="supporting">{caName || t.caHint}</Text>
                      </VStack>
                      {caName && (
                        <Button
                          label={t.remove}
                          variant="ghost"
                          size="sm"
                          onClick={() => {
                            setCAName("");
                            setCAPem("");
                            setCAChanged(true);
                          }}
                        />
                      )}
                      <Button label={t.chooseFile} size="sm" onClick={chooseCA} />
                    </HStack>
                    {caError && <Banner status="error" title={caError} />}
                  </VStack>
                </Collapsible>
              )}
            </VStack>
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <HStack gap={2} hAlign="end">
              <Button label={t.cancel} variant="secondary" onClick={onClose} />
              <Button label={action} variant="primary" isLoading={busy} onClick={submit} />
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}
