// Which messages the messages page selects, as a pure reducer over message keys.
import type { SelectHow } from "./MessageRow";

/**
 * Nothing, one message, or several. Several always means at least two: one
 * left is simply selected. Among several, the cursor is the message the
 * keyboard moves from, selected or not, or "" for none.
 */
export type Selected =
  | { kind: "none" }
  | { kind: "one"; key: string }
  | { kind: "many"; keys: ReadonlySet<string>; cursor: string };

export interface Selection {
  selected: Selected;
  /** Where a Shift selection starts: the message clicked last without Shift, or "". */
  anchor: string;
  /** Whether a narrow page shows the reading pane instead of the list. It always has a cursor. */
  reading: boolean;
}

export type SelectionAction =
  /** A click or an arrow key on key; order is the keys of the list. */
  | { type: "select"; key: string; how: SelectHow; order: readonly string[]; wide: boolean }
  | { type: "selectAll"; order: readonly string[]; wide: boolean }
  /** Ends a selection of several, or else leaves a narrow reading pane, or else selects nothing. */
  | { type: "escape"; wide: boolean }
  /** Ends a selection of several, keeping the cursor selected. */
  | { type: "clear" }
  /** Back from a narrow reading pane to the list. */
  | { type: "back" }
  /** A notification's message: it shows alone, and a Shift selection starts at it. */
  | { type: "notified" }
  | { type: "show"; key: string }
  /** The keys the page has now: the selection lets go of the others. */
  | { type: "prune"; present: ReadonlySet<string> }
  /** Several were deleted: the cursor moves to the message after them, or else before them. */
  | { type: "deleteMany"; keys: ReadonlySet<string>; order: readonly string[] }
  /** One was deleted: a cursor on it moves to its neighbor. */
  | { type: "deleteOne"; key: string; order: readonly string[] }
  /** Deleting key failed: a cursor still where the deletion moved it goes back from from. */
  | { type: "restore"; key: string; from: string };

const none: Selected = { kind: "none" };

export const emptySelection: Selection = { selected: none, anchor: "", reading: false };

/** The message selected, or the cursor among several, or "". */
export function cursorOf(s: Selected): string {
  return s.kind === "one" ? s.key : s.kind === "many" ? s.cursor : "";
}

/** The messages selected together, empty unless there are several. */
export function pickedOf(s: Selected): ReadonlySet<string> {
  return s.kind === "many" ? s.keys : noKeys;
}

const noKeys: ReadonlySet<string> = new Set();

function one(key: string): Selected {
  return key ? { kind: "one", key } : none;
}

/** Several keys with their cursor, or the one key, or nothing. */
function pick(keys: Iterable<string>, cursor: string): Selected {
  const set = new Set(keys);
  if (set.size > 1) return { kind: "many", keys: set, cursor };
  return one(set.values().next().value ?? "");
}

/** The same selection with its cursor on key. */
function moveCursor(s: Selected, key: string): Selected {
  return s.kind === "many" ? { ...s, cursor: key } : one(key);
}

function clearMany(s: Selected): Selected {
  return s.kind === "many" ? one(s.cursor) : s;
}

export function reduceSelection(s: Selection, a: SelectionAction): Selection {
  const sel = s.selected;
  switch (a.type) {
    case "select": {
      if (a.how === "only") return { selected: one(a.key), anchor: a.key, reading: true };
      let keys: Set<string>;
      let anchor = s.anchor;
      if (a.how === "toggle") {
        const cursor = cursorOf(sel);
        keys = new Set(sel.kind === "many" ? sel.keys : cursor ? [cursor] : []);
        if (keys.has(a.key)) keys.delete(a.key);
        else keys.add(a.key);
        anchor = a.key;
      } else {
        const to = a.order.indexOf(a.key);
        // Without an anchor, the selection starts at the message selected, and stays anchored there.
        anchor ||= cursorOf(sel);
        let from = a.order.indexOf(anchor);
        if (from < 0) {
          from = to;
          anchor = a.key;
        }
        keys = new Set(a.order.slice(Math.min(from, to), Math.max(from, to) + 1));
      }
      if (keys.size > 1) return { selected: pick(keys, a.key), anchor, reading: a.wide && s.reading };
      // One left: it is simply selected, and read where the pane shows it.
      const only = a.order.find((k) => keys.has(k)) ?? "";
      return { selected: one(only), anchor, reading: a.wide && only !== "" };
    }
    case "selectAll":
      if (a.order.length < 2) return s;
      return { ...s, selected: pick(a.order, cursorOf(sel)), reading: a.wide && s.reading };
    case "escape":
      if (sel.kind === "many") return { ...s, selected: clearMany(sel) };
      if (!a.wide && s.reading) return { ...s, reading: false };
      if (sel.kind === "one") return { ...s, selected: none, reading: false };
      return s;
    case "clear":
      return sel.kind === "many" ? { ...s, selected: clearMany(sel) } : s;
    case "back":
      return s.reading ? { ...s, reading: false } : s;
    case "notified":
      return { ...s, selected: clearMany(sel), anchor: "" };
    case "show":
      return { ...s, selected: one(a.key), reading: true };
    case "prune": {
      const cursor = cursorOf(sel);
      const gone = !a.present.has(cursor);
      // The cursor's message went away, and the reading pane with it.
      const lost = cursor !== "" && gone;
      const reading = s.reading && !lost;
      if (sel.kind !== "many") return lost ? { ...s, selected: none, reading } : s;
      const kept = [...sel.keys].filter((k) => a.present.has(k));
      if (kept.length === sel.keys.size && !lost) return s;
      // The cursor stays on a message still selected, and one left is simply selected.
      if (kept.length > 0) return { ...s, selected: pick(kept, gone ? kept[0]! : cursor), reading };
      return { ...s, selected: one(gone ? "" : cursor), reading };
    }
    case "deleteMany": {
      const last = a.order.reduce((at, k, i) => (a.keys.has(k) ? i : at), -1);
      const rest = (k: string) => !a.keys.has(k);
      const next = a.order.slice(last + 1).find(rest) ?? a.order.slice(0, last).reverse().find(rest) ?? "";
      return { ...s, selected: one(next), reading: s.reading && next !== "" };
    }
    case "deleteOne": {
      if (cursorOf(sel) !== a.key) return s;
      const i = a.order.indexOf(a.key);
      const next = i < 0 ? "" : (a.order[i + 1] ?? a.order[i - 1] ?? "");
      return { ...s, selected: moveCursor(sel, next), reading: s.reading && next !== "" };
    }
    case "restore":
      return cursorOf(sel) === a.from ? { ...s, selected: moveCursor(sel, a.key) } : s;
  }
}
