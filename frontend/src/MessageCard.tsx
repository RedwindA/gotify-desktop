import { ContextMenu } from "@astryxdesign/core/ContextMenu";
import { Lightbox } from "@astryxdesign/core/Lightbox";
import { Markdown } from "@astryxdesign/core/Markdown";
import { Text } from "@astryxdesign/core/Text";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { Desktop, type Message } from "./mygo";
import { useT } from "./i18n";

const urlPattern = /\bhttps?:\/\/[^\s<>"')\]]+[^\s<>"')\].,;:!?]/g;

export function openMessageLink(href: string) {
  if (!href) return;
  void Desktop.openURL(href).catch(() => {});
}

/** One line for the list: images and links become their text, and markdown markers go. */
export function previewText(body: string): string {
  return body
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/`([^`]*)`/g, "$1")
    .replace(/!\[[^\]]*]\([^)]*\)/g, " ")
    .replace(/\[([^\]]*)]\([^)]*\)/g, "$1")
    .replace(/^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)*\|?\s*$/gm, " ")
    .replace(/\s*\|\s*/g, " ")
    .replace(/[#*_>]+/g, "")
    .replace(/\s+/g, " ")
    .trim();
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
          openMessageLink(href);
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

export function MessageBody({ msg, title }: { msg: Message; title: string }) {
  const [viewing, setViewing] = useState<string | null>(null);
  return (
    <>
      {msg.body &&
        (msg.markdown ? (
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
                openMessageLink(href);
                return false;
              }}>
              {msg.body}
            </Markdown>
          </div>
        ) : (
          <PlainBody text={msg.body} />
        ))}
      {msg.imageSrc && !(msg.markdown && msg.body.includes(msg.imageUrl)) && <MessageImage src={msg.imageSrc} onView={setViewing} />}
      {viewing && (
        <Lightbox isOpen onOpenChange={(open) => !open && setViewing(null)} media={{ src: viewing, alt: title, caption: title }} hasZoom />
      )}
    </>
  );
}

function restoreSelection(range: Range) {
  if (!range.startContainer.isConnected || !range.endContainer.isConnected) return;
  const selection = window.getSelection();
  selection?.removeAllRanges();
  selection?.addRange(range);
}

/**
 * The row and the reading pane share one menu. The selection is taken inside `root` only,
 * when the contextmenu event arrives. WebKit drops it while the menu is open, so it is put
 * back whenever it goes, and once more after the menu closes, unless a click closed it.
 */
export function useMessageMenu(msg: Message, root: { readonly current: HTMLElement | null }, deleting: boolean, onDelete: (msg: Message) => void) {
  const t = useT();
  const [selectedText, setSelectedText] = useState("");
  const kept = useRef<Range | null>(null);
  const pressedOutside = useRef(false);
  const stopWatching = useRef<(() => void) | null>(null);
  useEffect(() => {
    const el = root.current;
    if (!el) return;
    // Never copy a different message's selection.
    const onMenu = () => {
      const selection = window.getSelection();
      const inside =
        selection && selection.rangeCount > 0 && !selection.isCollapsed && el.contains(selection.anchorNode) && el.contains(selection.focusNode);
      kept.current = inside ? selection.getRangeAt(0).cloneRange() : null;
      setSelectedText(inside ? selection.toString() : "");
    };
    el.addEventListener("contextmenu", onMenu, true);
    return () => el.removeEventListener("contextmenu", onMenu, true);
  }, [root]);
  const onOpenChange = useCallback((open: boolean) => {
    stopWatching.current?.();
    stopWatching.current = null;
    const range = kept.current;
    if (!range) return;
    if (!open) {
      kept.current = null;
      // A press outside the menu closed it, and that press placed a selection of its own.
      // Otherwise restore after the menu hands focus back.
      if (!pressedOutside.current) setTimeout(() => restoreSelection(range));
      return;
    }
    // WebKit clears it as the menu focuses an item and again as the right button comes up.
    pressedOutside.current = false;
    const onPress = (e: MouseEvent) => {
      if (!(e.target instanceof Element && e.target.closest('[role="menu"]'))) pressedOutside.current = true;
    };
    const onSelectionChange = () => {
      if (!pressedOutside.current && window.getSelection()?.isCollapsed !== false) restoreSelection(range);
    };
    document.addEventListener("mousedown", onPress, true);
    document.addEventListener("selectionchange", onSelectionChange);
    stopWatching.current = () => {
      document.removeEventListener("mousedown", onPress, true);
      document.removeEventListener("selectionchange", onSelectionChange);
    };
  }, []);
  useEffect(() => () => stopWatching.current?.(), []);
  const items = [
    ...(selectedText ? [{ label: t.copySelectedText, onClick: () => void Desktop.copyText(selectedText) }] : []),
    { label: t.copyText, onClick: () => void Desktop.copyText(msg.body) },
    ...(msg.clickUrl ? [{ label: t.openLink, onClick: () => openMessageLink(msg.clickUrl) }] : []),
    ...(msg.imageUrl ? [{ label: t.openImageInBrowser, onClick: () => openMessageLink(msg.imageUrl) }] : []),
    { type: "divider" as const },
    { label: t.delete, variant: "destructive" as const, isDisabled: deleting, onClick: () => onDelete(msg) },
  ];
  return { items, onOpenChange };
}

export function MessageMenu({
  msg,
  deleting,
  onDelete,
  children,
}: {
  msg: Message;
  deleting: boolean;
  onDelete(msg: Message): void;
  children: ReactNode;
}) {
  const t = useT();
  const root = useRef<HTMLDivElement>(null);
  const { items, onOpenChange } = useMessageMenu(msg, root, deleting, onDelete);
  return (
    <ContextMenu ref={root} items={items} label={t.messageActions} onOpenChange={onOpenChange}>
      {children}
    </ContextMenu>
  );
}
