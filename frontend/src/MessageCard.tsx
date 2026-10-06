import { Badge } from "@astryxdesign/core/Badge";
import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { ContextMenu } from "@astryxdesign/core/ContextMenu";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Lightbox } from "@astryxdesign/core/Lightbox";
import { Markdown } from "@astryxdesign/core/Markdown";
import { MoreMenu } from "@astryxdesign/core/MoreMenu";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { Text } from "@astryxdesign/core/Text";
import { Timestamp } from "@astryxdesign/core/Timestamp";
import { ExternalLinkIcon } from "lucide-react";
import { memo, useCallback, useRef, useState, type ReactNode } from "react";
import { Desktop, type App, type Message } from "./mygo";
import { useT } from "./i18n";
import { AppAvatar } from "./Sidebar";

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
    <Text as="p" display="block" className="msg-plain selectable">
      {parts}
    </Text>
  );
}

/**
 * The image a message shows below its body, which Go serves; clicking it opens
 * the viewer. It holds the room of a placeholder until it loaded, and goes
 * away when it fails.
 */
function MessageImage({ src, onView }: { src: string; onView(src: string): void }) {
  const [state, setState] = useState<"loading" | "loaded" | "failed">("loading");
  if (state === "failed") return null;
  return (
    <img
      className={"msg-image" + (state === "loading" ? " msg-image-loading" : "")}
      src={src}
      alt=""
      decoding="async"
      aria-busy={state === "loading" || undefined}
      onLoad={() => setState("loaded")}
      onError={() => setState("failed")}
      onClick={() => state === "loaded" && onView(src)}
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
  const [viewing, setViewing] = useState<string | null>(null);
  const card = useRef<HTMLDivElement>(null);
  const [selectedText, setSelectedText] = useState("");
  const captureSelection = useCallback((open: boolean) => {
    if (!open) return;
    // Snapshot before the menu takes focus; never copy a different card's selection.
    const selection = window.getSelection();
    setSelectedText(selection && !selection.isCollapsed && card.current?.contains(selection.anchorNode) && card.current?.contains(selection.focusNode)
      ? selection.toString() : "");
  }, []);
  const title = msg.title || app?.name || t.message;
  const items = [
    ...(selectedText ? [{ label: t.copySelectedText, onClick: () => void Desktop.copyText(selectedText) }] : []),
    { label: t.copyText, onClick: () => void Desktop.copyText(msg.body) },
    ...(msg.clickUrl ? [{ label: t.openLink, onClick: () => openLink(msg.clickUrl) }] : []),
    ...(msg.imageUrl ? [{ label: t.openImageInBrowser, onClick: () => openLink(msg.imageUrl) }] : []),
    { type: "divider" as const },
    { label: t.delete, variant: "destructive" as const, isDisabled: deleting, onClick: () => onDelete(msg) },
  ];
  const meta = [app?.name, serverName].filter(Boolean).join(" · ");
  return (
    <>
      <ContextMenu items={items} label={t.messageActions} onOpenChange={captureSelection}>
        <Card
          ref={card}
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
                    <Text weight="semibold" maxLines={1} className="selectable">
                      {title}
                    </Text>
                  </HStack>
                  <HStack gap={1} vAlign="center">
                    {meta && <Text type="supporting">{meta} ·</Text>}
                    <Timestamp value={msg.date} format="auto" isLive />
                  </HStack>
                </VStack>
                {msg.priority >= 8 && <Badge variant="error" label={t.priority(msg.priority)} className="no-shrink" />}
                <MoreMenu size="sm" label={t.messageActions} alignment="end" items={items} onOpenChange={captureSelection} />
              </HStack>
              {msg.body &&
                (msg.markdown ? (
                  // Images in the body open in the viewer too.
                  <div
                    className="selectable"
                    onClick={(e) => {
                      if (e.target instanceof HTMLImageElement) setViewing(e.target.currentSrc || e.target.src);
                    }}>
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
                  </div>
                ) : (
                  <PlainBody text={msg.body} />
                ))}
              {msg.imageSrc && !(msg.markdown && msg.body.includes(msg.imageUrl)) && <MessageImage src={msg.imageSrc} onView={setViewing} />}
              {msg.clickUrl && (
                <HStack>
                  <Button label={t.openLink} size="sm" icon={<ExternalLinkIcon size={14} />} tooltip={msg.clickUrl} onClick={() => openLink(msg.clickUrl)} />
                </HStack>
              )}
            </VStack>
          </HStack>
        </Card>
      </ContextMenu>
      {viewing && (
        <Lightbox
          isOpen
          onOpenChange={(open) => !open && setViewing(null)}
          media={{ src: viewing, alt: title, caption: title }}
          hasZoom
        />
      )}
    </>
  );
});
