import { Badge } from "@astryxdesign/core/Badge";
import { Button } from "@astryxdesign/core/Button";
import { ContextMenu } from "@astryxdesign/core/ContextMenu";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Text } from "@astryxdesign/core/Text";
import { Timestamp } from "@astryxdesign/core/Timestamp";
import { CheckIcon, CopyIcon, ExternalLinkIcon, ImageIcon, Trash2Icon } from "lucide-react";
import { memo, useEffect, useRef, useState } from "react";
import { MessageBody, openMessageLink, useMessageMenu } from "./MessageCard";
import { Desktop, type App, type Message } from "./mygo";
import { useT } from "./i18n";
import { AppAvatar } from "./Sidebar";

export interface MessageViewProps {
  msg: Message;
  app: App | undefined;
  serverName: string | undefined;
  deleting: boolean;
  onDelete(msg: Message): void;
}

export const MessageView = memo(function MessageView({ msg, app, serverName, deleting, onDelete }: MessageViewProps) {
  const t = useT();
  const root = useRef<HTMLDivElement>(null);
  const { items, onOpenChange } = useMessageMenu(msg, root, deleting, onDelete);
  // Counts copies; the check shows while it is non-zero and pops again on each copy.
  const [copied, setCopied] = useState(0);
  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(0), 1500);
    return () => clearTimeout(timer);
  }, [copied]);
  const title = msg.title || app?.name || t.message;
  const meta = [app?.name, serverName].filter(Boolean).join(" · ");
  return (
    <div ref={root} className={"msg-view" + (deleting ? " msg-deleting" : "")}>
      <div className="msg-view-fill">
        <div className="msg-view-bar">
          <HStack gap={1} vAlign="center">
            {msg.clickUrl && (
              <Button
                label={t.openLink}
                size="sm"
                variant="ghost"
                isIconOnly
                icon={<ExternalLinkIcon size={16} />}
                tooltip={msg.clickUrl}
                onClick={() => openMessageLink(msg.clickUrl)}
              />
            )}
            <Button
              label={copied ? t.copied : t.copyText}
              size="sm"
              variant="ghost"
              isIconOnly
              icon={copied ? <CheckIcon key={copied} size={16} className="copy-done" /> : <CopyIcon size={16} />}
              tooltip={t.copyText}
              onClick={() =>
                void Desktop.copyText(msg.body)
                  .then(() => setCopied((n) => n + 1))
                  .catch(() => {})
              }
            />
            {msg.imageUrl && (
              <Button
                label={t.openImageInBrowser}
                size="sm"
                variant="ghost"
                isIconOnly
                icon={<ImageIcon size={16} />}
                tooltip={t.openImageInBrowser}
                onClick={() => openMessageLink(msg.imageUrl)}
              />
            )}
            <Button
              label={t.delete}
              size="sm"
              variant="ghost"
              isIconOnly
              icon={<Trash2Icon size={16} />}
              tooltip={t.delete}
              isDisabled={deleting}
              onClick={() => onDelete(msg)}
            />
          </HStack>
        </div>
        <ContextMenu items={items} label={t.messageActions} onOpenChange={onOpenChange}>
          <div className="msg-read-scroll">
            <VStack gap={4} className="msg-view-body">
              <HStack gap={3} vAlign="start">
                <div className="no-shrink">
                  <AppAvatar serverId={msg.serverId} app={app} size="md" />
                </div>
                <VStack gap={1} className="grow">
                  <Text weight="semibold" size="lg" className="selectable msg-view-title">
                    {title}
                  </Text>
                  <HStack gap={2} vAlign="center">
                    {meta && (
                      <Text type="supporting" maxLines={1}>
                        {meta}
                      </Text>
                    )}
                    <Timestamp value={msg.date} format="date_time" className="no-shrink" />
                    {msg.priority >= 8 && <Badge variant="error" label={t.priority(msg.priority)} className="no-shrink" />}
                  </HStack>
                </VStack>
              </HStack>
              <MessageBody msg={msg} title={title} />
            </VStack>
          </div>
        </ContextMenu>
      </div>
    </div>
  );
});
