import { ContextMenu } from "@astryxdesign/core/ContextMenu";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useT } from "./i18n";
import { Desktop } from "./mygo";

type Field = HTMLInputElement | HTMLTextAreaElement;

/** The field right-clicked and its selection, taken as the menu opens. */
interface Snapshot {
  field: Field;
  start: number;
  end: number;
  editable: boolean;
  secret: boolean;
  empty: boolean;
}

function textField(target: EventTarget | null): Field | null {
  const el = target instanceof Element ? target.closest("input, textarea") : null;
  if (el instanceof HTMLTextAreaElement) return el;
  // setSelectionRange works on these types only; a checkbox or a switch has no text to edit.
  if (el instanceof HTMLInputElement && /^(text|search|url|tel|password)$/.test(el.type)) return el;
  return null;
}

/** Gives the field its focus and selection back, which the menu took while it was open. */
function restore(s: Snapshot) {
  s.field.focus({ preventScroll: true });
  s.field.setSelectionRange(s.start, s.end);
}

/**
 * Replaces the selection the way typing does, so the field's undo history and
 * React's onChange both see it. execCommand is the only way to reach the undo
 * history; where it is refused, the text is set directly and announced.
 */
function replaceSelection(s: Snapshot, text: string) {
  restore(s);
  if (document.execCommand(text ? "insertText" : "delete", false, text)) return;
  s.field.setRangeText(text, s.start, s.end, "end");
  s.field.dispatchEvent(new InputEvent("input", { bubbles: true, inputType: text ? "insertFromPaste" : "deleteByCut", data: text || null }));
}

/**
 * The app's own edit menu for the text fields in `children`, in place of the
 * webview's. The clipboard goes through the desktop: a webview keeps pages from
 * reading it, or asks the user first. A right click elsewhere in `children`,
 * on a label or a disabled field, shows no menu at all.
 */
export function FieldMenu({ children }: { children: ReactNode }) {
  const t = useT();
  const root = useRef<HTMLDivElement>(null);
  const [snap, setSnap] = useState<Snapshot | null>(null);

  useEffect(() => {
    const el = root.current;
    if (!el) return;
    const onMenu = (e: MouseEvent) => {
      const field = textField(e.target);
      if (!field || field.disabled || !el.contains(field)) {
        // Neither this menu (whose handler is past this capture listener) nor the webview's.
        e.preventDefault();
        e.stopPropagation();
        return;
      }
      const start = field.selectionStart ?? 0;
      const end = field.selectionEnd ?? start;
      setSnap({ field, start, end, editable: !field.readOnly, secret: field.type === "password", empty: field.value === "" });
    };
    el.addEventListener("contextmenu", onMenu, true);
    return () => el.removeEventListener("contextmenu", onMenu, true);
  }, []);

  const selected = snap !== null && snap.start !== snap.end;
  const selection = () => (snap ? snap.field.value.slice(snap.start, snap.end) : "");
  const items = [
    {
      label: t.cut,
      isDisabled: !snap?.editable || snap.secret || !selected,
      onClick: () => {
        if (!snap) return;
        void Desktop.copyText(selection())
          .then(() => replaceSelection(snap, ""))
          .catch(() => {});
      },
    },
    {
      label: t.copy,
      isDisabled: !selected || snap.secret,
      onClick: () => void Desktop.copyText(selection()).catch(() => {}),
    },
    {
      label: t.paste,
      isDisabled: !snap?.editable,
      onClick: () => {
        if (!snap) return;
        void Desktop.readText()
          .then((text) => {
            // A single-line field takes the lines joined, as a native paste does.
            const clean = snap.field instanceof HTMLInputElement ? text.replace(/\r\n|\r|\n/g, " ") : text;
            if (clean) replaceSelection(snap, clean);
          })
          .catch(() => {});
      },
    },
    { type: "divider" as const },
    {
      label: t.selectAll,
      isDisabled: !snap || snap.empty,
      onClick: () => {
        if (!snap) return;
        snap.field.focus({ preventScroll: true });
        snap.field.select();
      },
    },
  ];

  return (
    <ContextMenu ref={root} items={items} label={t.editActions}>
      {children}
    </ContextMenu>
  );
}
