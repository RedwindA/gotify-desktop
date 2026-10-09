import { Badge } from "@astryxdesign/core/Badge";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { Text } from "@astryxdesign/core/Text";
import { Timestamp } from "@astryxdesign/core/Timestamp";
import { memo } from "react";
import { MessageMenu, previewText } from "./MessageCard";
import { type App, type Message } from "./mygo";
import { useT } from "./i18n";
import { AppAvatar } from "./Sidebar";

export interface MessageRowProps {
  msg: Message;
  app: App | undefined;
  serverName: string | undefined;
  highlighted: boolean;
  deleting: boolean;
  selected: boolean;
  unread: boolean;
  tabIndex: number;
  onSelect(msg: Message): void;
  onDelete(msg: Message): void;
}

export const MessageRow = memo(function MessageRow({
  msg,
  app,
  serverName,
  highlighted,
  deleting,
  selected,
  unread,
  tabIndex,
  onSelect,
  onDelete,
}: MessageRowProps) {
  const t = useT();
  const title = msg.title || app?.name || t.message;
  const meta = [app?.name, serverName].filter(Boolean).join(" · ");
  const preview = previewText(msg.body);
  return (
    <MessageMenu msg={msg} deleting={deleting} onDelete={onDelete}>
      <HStack
        gap={2}
        vAlign="center"
        padding={2}
        role="option"
        aria-selected={selected}
        tabIndex={tabIndex}
        data-key={`${msg.serverId}/${msg.id}`}
        data-unread={unread ? "true" : undefined}
        className={"msg-option" + (highlighted ? " msg-highlight" : "") + (deleting ? " msg-deleting" : "")}
        onPointerUp={(e) => {
          if (e.button !== 0) return;
          onSelect(msg);
        }}>
        <div className="no-shrink">
          <AppAvatar serverId={msg.serverId} app={app} size="sm" />
        </div>
        <VStack gap={0} className="grow">
          <HStack gap={2} vAlign="center">
            <span className="msg-unread">{unread && <StatusDot variant="accent" label={t.unread} />}</span>
            <Text weight={unread ? "semibold" : "normal"} maxLines={1} hasTruncateTooltip={false} wordBreak="break-word" className="selectable grow">
              {title}
            </Text>
            {msg.priority >= 8 && <Badge variant="error" label={t.priority(msg.priority)} className="no-shrink" />}
            <Timestamp value={msg.date} format="relative_short" isLive hasTooltip={false} className="no-shrink" />
          </HStack>
          {meta && (
            <Text type="supporting" maxLines={1} hasTruncateTooltip={false}>
              {meta}
            </Text>
          )}
          {preview && (
            <Text type="supporting" color="secondary" maxLines={1} hasTruncateTooltip={false}>
              {preview}
            </Text>
          )}
        </VStack>
      </HStack>
    </MessageMenu>
  );
});
