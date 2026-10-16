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

/** How a click selects: this message only, adding or removing it (Ctrl/⌘), or up to it (Shift). */
export type SelectHow = "only" | "toggle" | "range";

const isMac = /Mac/.test(navigator.userAgent);

function selectHow(e: { shiftKey: boolean; ctrlKey: boolean; metaKey: boolean }): SelectHow {
  if (e.shiftKey) return "range";
  return (isMac ? e.metaKey : e.ctrlKey) ? "toggle" : "only";
}

export interface MessageRowProps {
  msg: Message;
  app: App | undefined;
  serverName: string | undefined;
  highlighted: boolean;
  deleting: boolean;
  selected: boolean;
  unread: boolean;
  tabIndex: number;
  /** How many messages are selected with this one, or 0 when it is not among several. */
  bulkCount: number;
  onSelect(msg: Message, how: SelectHow): void;
  onDelete(msg: Message): void;
  onBulkRead(): void;
  onBulkDelete(): void;
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
  bulkCount,
  onSelect,
  onDelete,
  onBulkRead,
  onBulkDelete,
}: MessageRowProps) {
  const t = useT();
  const title = msg.title || app?.name || t.message;
  const meta = [app?.name, serverName].filter(Boolean).join(" · ");
  const preview = previewText(msg.body);
  return (
    <MessageMenu msg={msg} deleting={deleting} onDelete={onDelete} bulkCount={bulkCount} onBulkRead={onBulkRead} onBulkDelete={onBulkDelete}>
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
        onMouseDown={(e) => {
          // Selecting several must not select text.
          if (e.button === 0 && selectHow(e) !== "only") e.preventDefault();
        }}
        onPointerUp={(e) => {
          if (e.button !== 0) return;
          onSelect(msg, selectHow(e));
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
