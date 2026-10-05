import { Button } from "@astryxdesign/core/Button";
import { Heading } from "@astryxdesign/core/Heading";
import { HStack, Layout, LayoutContent, LayoutHeader, VStack } from "@astryxdesign/core/Layout";
import { Divider } from "@astryxdesign/core/Divider";
import { Section } from "@astryxdesign/core/Section";
import { Selector } from "@astryxdesign/core/Selector";
import { Switch } from "@astryxdesign/core/Switch";
import { Text } from "@astryxdesign/core/Text";
import { TimeInput, type ISOTimeString } from "@astryxdesign/core/TimeInput";
import type { ReactNode } from "react";
import { useLang, useT } from "./i18n";
import { Desktop, type Language, type State, type Theme } from "./mygo";
import { errorText, refreshState, useNow, useSettings } from "./store";

const toTime = (min: number) => `${String(Math.floor(min / 60)).padStart(2, "0")}:${String(min % 60).padStart(2, "0")}` as ISOTimeString;

function toMinutes(t: string | undefined): number | null {
  const m = t && /^(\d{2}):(\d{2})/.exec(t);
  return m ? Number(m[1]) * 60 + Number(m[2]) : null;
}

function tomorrowMorning(): Date {
  const d = new Date();
  d.setDate(d.getDate() + 1);
  d.setHours(8, 0, 0, 0);
  return d;
}


function Group({ title, children }: { title: string; children: ReactNode }) {
  return (
    <VStack gap={2}>
      <Heading level={2}>{title}</Heading>
      <Section padding={4} className="settings-group">
        <VStack gap={4}>{children}</VStack>
      </Section>
    </VStack>
  );
}

export function SettingsPage({ state, onError, onInfo }: { state: State; onError(msg: string): void; onInfo(msg: string): void }) {
  const t = useT();
  const lang = useLang();
  const [s, update] = useSettings(state.settings, (e) => onError(t.couldNotSaveSettings(e)));
  const pausedFormat = new Intl.DateTimeFormat(lang, { weekday: "short", hour: "2-digit", minute: "2-digit" });
  const now = useNow(30_000);
  const paused = s.pausedUntil !== null && Date.parse(s.pausedUntil) > now;

  return (
    <Layout
      contentWidth={680}
      padding={5}
      header={
        <LayoutHeader hasDivider className="titlebar">
          <Heading level={1}>{t.settings}</Heading>
        </LayoutHeader>
      }
      content={
        <LayoutContent>
          <VStack gap={6}>
            <Group title={t.general}>
              <Selector
                label={t.language}
                options={[
                  { value: "system", label: t.languageSystem },
                  { value: "zh-CN", label: "简体中文" },
                  { value: "en", label: "English" },
                ]}
                value={s.language || "system"}
                width={240}
                onChange={(v) => update({ language: (v === "system" ? "" : v) as Language })}
              />
              <Selector
                label={t.appearance}
                options={[
                  { value: "system", label: t.appearanceSystem },
                  { value: "light", label: t.appearanceLight },
                  { value: "dark", label: t.appearanceDark },
                ]}
                value={s.theme || "system"}
                width={240}
                onChange={(v) => update({ theme: (v === "system" ? "" : v) as Theme })}
              />
              <Switch
                label={t.startAtLogin}
                description={t.startAtLoginText}
                value={state.openAtLogin}
                labelPosition="start"
                labelSpacing="spread"
                width="100%"
                changeAction={async (on) => {
                  try {
                    await Desktop.setOpenAtLogin(on);
                  } catch (err) {
                    onError(t.couldNotLoginItem(errorText(err)));
                  }
                  await refreshState();
                }}
              />
            </Group>

            <Group title={t.notifications}>
              <HStack gap={3} vAlign="center" wrap="wrap">
                <VStack gap={0.5} className="grow">
                  <Text weight="medium">{paused ? t.notificationsPaused : t.notificationsOn}</Text>
                  <Text type="supporting">
                    {paused ? t.resumesAt(pausedFormat.format(new Date(s.pausedUntil!))) : t.notificationsOnText}
                  </Text>
                </VStack>
                {paused ? (
                  <Button label={t.resume} variant="primary" onClick={() => update({ pausedUntil: null })} />
                ) : (
                  <>
                    <Button label={t.pauseHour} onClick={() => update({ pausedUntil: new Date(Date.now() + 3600_000).toISOString() })} />
                    <Button label={t.pauseTomorrow} onClick={() => update({ pausedUntil: tomorrowMorning().toISOString() })} />
                  </>
                )}
              </HStack>
              <Divider />
              <Switch
                label={t.dnd}
                description={t.dndText}
                value={s.dnd}
                labelPosition="start"
                labelSpacing="spread"
                width="100%"
                onChange={(dnd) => update({ dnd })}
              />
              <HStack gap={3} vAlign="end">
                <TimeInput
                  label={t.from}
                  hourFormat="24h"
                  value={toTime(s.dndStart)}
                  isDisabled={!s.dnd}
                  width={140}
                  onChange={(v) => {
                    const m = toMinutes(v);
                    if (m !== null) update({ dndStart: m });
                  }}
                />
                <TimeInput
                  label={t.to}
                  hourFormat="24h"
                  value={toTime(s.dndEnd)}
                  isDisabled={!s.dnd}
                  width={140}
                  onChange={(v) => {
                    const m = toMinutes(v);
                    if (m !== null) update({ dndEnd: m });
                  }}
                />
              </HStack>
              <Switch
                label={t.highBypass}
                description={t.highBypassText}
                value={s.highBypassesDnd}
                isDisabled={!s.dnd}
                labelPosition="start"
                labelSpacing="spread"
                width="100%"
                onChange={(highBypassesDnd) => update({ highBypassesDnd })}
              />
              <Divider />
              <HStack gap={3} vAlign="center">
                <VStack gap={0.5} className="grow">
                  <Text weight="medium">{t.testTitle}</Text>
                  <Text type="supporting">{t.testText}</Text>
                </VStack>
                <Button
                  label={t.sendTest}
                  clickAction={async () => {
                    try {
                      await Desktop.testNotification();
                      onInfo(t.testSent);
                    } catch (err) {
                      onError(t.notificationsUnavailable(errorText(err)));
                    }
                  }}
                />
              </HStack>
            </Group>

            <Group title={t.about}>
              <VStack gap={1}>
                <Text weight="medium">Gotify Desktop {state.version}</Text>
                <Text type="supporting">
                  {t.servers(state.servers.length)} · {t.dataIn}{" "}
                  <Text type="code" size="sm" className="selectable">
                    {state.dataDir}
                  </Text>
                </Text>
              </VStack>
            </Group>
          </VStack>
        </LayoutContent>
      }
    />
  );
}
