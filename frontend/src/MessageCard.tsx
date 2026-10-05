import { Badge } from "@astryxdesign/core/Badge";
import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { ContextMenu } from "@astryxdesign/core/ContextMenu";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Markdown } from "@astryxdesign/core/Markdown";
import { MoreMenu } from "@astryxdesign/core/MoreMenu";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { Text } from "@astryxdesign/core/Text";
import { Timestamp } from "@astryxdesign/core/Timestamp";
import { ExternalLinkIcon } from "lucide-react";
import { memo, type ReactNode } from "react";
import { Desktop, type App, type Message } from "./mygo";
import { useT } from "./i18n";
import { AppAvatar } from "./Sidebar";
import { useMessageImage } from "./store";

const urlPattern = /\bhttps?:\/\/[^\s<>"')\]]+[^\s<>"')\].,;:!?]/g;

function openLink(href: string) {
  void Desktop.openURL(href).catch(() => {});
}

/** Plain text with its URLs as links; newlines are kept by the CSS. */
function PlainBody({ text }: { text: string }) {
  const parts: ReactNode[] = [];
  let last = 0;
  for (const m of text.matchAll(urlPattern)) {
    parts.push(text.slice(last, m.index));
    const href = m[0];
    parts.push(
      <a
        key={m.index}
        href={href}
        onClick={(e) => {
          e.preventDefault();
          openLink(href);
        }}>
        {href}
      </a>,
    );
    last = m.index + href.length;
  }
  parts.push(text.slice(last));
  return (
    <Text as="p" display="block" className="msg-plain">
      {parts}
    </Text>
  );
}

/** The image a message shows below its body; clicking it opens the original. */
function MessageImage({ msg }: { msg: Message }) {
  const t = useT();
  const src = useMessageImage(msg.serverId, msg.id, msg.imageUrl);
  if (src === "") return null;
  if (src === undefined) return <div className="msg-image msg-image-loading" aria-busy="true" />;
  return (
    <img
      className="msg-image"
      src={src}
      alt={msg.title || t.image}
      title={msg.imageUrl}
      onClick={() => openLink(msg.imageUrl)}
    />
  );
}

export interface MessageCardProps {
  msg: Message;
  app: App | undefined;
  serverName: string | undefined;
  highlighted: boolean;
  deleting: boolean;
  onDelete(msg: Message): void;
}

export const MessageCard = memo(function MessageCard({ msg, app, serverName, highlighted, deleting, onDelete }: MessageCardProps) {
  const t = useT();
  const title = msg.title || app?.name || t.message;
  const items = [
    { label: t.copyText, onClick: () => void Desktop.copyText(msg.body) },
    ...(msg.clickUrl ? [{ label: t.openLink, onClick: () => openLink(msg.clickUrl) }] : []),
    ...(msg.imageUrl ? [{ label: t.openImage, onClick: () => openLink(msg.imageUrl) }] : []),
    { type: "divider" as const },
    { label: t.delete, variant: "destructive" as const, isDisabled: deleting, onClick: () => onDelete(msg) },
  ];
  const meta = [app?.name, serverName].filter(Boolean).join(" · ");
  return (
    <ContextMenu items={items} label={t.messageActions}>
      <Card
        className={"msg-card" + (highlighted ? " msg-highlight" : "") + (deleting ? " msg-deleting" : "")}
        data-key={`${msg.serverId}/${msg.id}`}
        data-unread={msg.read ? undefined : "true"}>
        <HStack gap={3} vAlign="start">
          <AppAvatar serverId={msg.serverId} app={app} size="md" />
          <VStack gap={2} className="msg-main">
            <HStack gap={2} vAlign="center">
              <VStack gap={0.5} className="msg-main">
                <HStack gap={2} vAlign="center">
                  {!msg.read && <StatusDot variant="accent" label={t.unread} />}
                  <Text weight="semibold" maxLines={1}>
                    {title}
                  </Text>
                </HStack>
                <HStack gap={1} vAlign="center">
                  {meta && <Text type="supporting">{meta} ·</Text>}
                  <Timestamp value={msg.date} format="auto" isLive />
                </HStack>
              </VStack>
              {msg.priority >= 8 && <Badge variant="error" label={t.priority(msg.priority)} className="no-shrink" />}
              <MoreMenu size="sm" label={t.messageActions} alignment="end" items={items} />
            </HStack>
            {msg.body &&
              (msg.markdown ? (
                <Markdown
                  density="compact"
                  contentWidth="100%"
                  headingLevelStart={4}
                  onLinkClick={(href) => {
                    openLink(href);
                    return false;
                  }}>
                  {msg.body}
                </Markdown>
              ) : (
                <PlainBody text={msg.body} />
              ))}
            {msg.imageUrl && !(msg.markdown && msg.body.includes(msg.imageUrl)) && <MessageImage msg={msg} />}
            {msg.clickUrl && (
              <HStack>
                <Button label={t.openLink} size="sm" icon={<ExternalLinkIcon size={14} />} tooltip={msg.clickUrl} onClick={() => openLink(msg.clickUrl)} />
              </HStack>
            )}
          </VStack>
        </HStack>
      </Card>
    </ContextMenu>
  );
});
