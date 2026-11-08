import { describe, expect, test } from "bun:test";
import { cursorOf, emptySelection, pickedOf, reduceSelection, type Selection, type SelectionAction } from "../src/selection";

const order = ["a", "b", "c", "d", "e"];

/** Runs actions from s, checking the invariants after each one. */
function run(s: Selection, ...actions: SelectionAction[]): Selection {
  for (const a of actions) {
    s = reduceSelection(s, a);
    if (s.selected.kind === "many") expect(s.selected.keys.size).toBeGreaterThan(1);
    if (s.selected.kind === "one") expect(s.selected.key).not.toBe("");
    if (s.reading) expect(cursorOf(s.selected)).not.toBe("");
  }
  return s;
}

const only = (key: string, wide = true): SelectionAction => ({ type: "select", key, how: "only", order, wide });
const toggle = (key: string, wide = true): SelectionAction => ({ type: "select", key, how: "toggle", order, wide });
const range = (key: string, wide = true): SelectionAction => ({ type: "select", key, how: "range", order, wide });
const keys = (s: Selection) => [...pickedOf(s.selected)];
const many = (picked: string[], cursor: string, wide = true) => run(emptySelection, only(cursor, wide), ...picked.filter((k) => k !== cursor).map((k) => toggle(k, wide)));

describe("select", () => {
  test("only selects one, reads it and anchors there", () => {
    const s = run(emptySelection, only("b"));
    expect(s).toEqual({ selected: { kind: "one", key: "b" }, anchor: "b", reading: true });
    expect(run(many(["a", "c"], "a"), only("d")).selected).toEqual({ kind: "one", key: "d" });
  });

  test("toggle adds to the one selected and removes again", () => {
    let s = run(emptySelection, only("a"), toggle("c"));
    expect(s.selected).toEqual({ kind: "many", keys: new Set(["a", "c"]), cursor: "c" });
    expect(s.anchor).toBe("c");
    s = run(s, toggle("d"), toggle("c"));
    // The cursor stays on the message toggled off.
    expect(s.selected).toEqual({ kind: "many", keys: new Set(["a", "d"]), cursor: "c" });
  });

  test("toggle down to one selects it and reads it in a wide page", () => {
    const s = run(emptySelection, only("a"), toggle("c"), toggle("a"));
    expect(s.selected).toEqual({ kind: "one", key: "c" });
    expect(s.reading).toBe(true);
    expect(run(emptySelection, only("a", false), toggle("c", false), toggle("a", false)).reading).toBe(false);
  });

  test("toggle off the only one selects nothing", () => {
    const s = run(emptySelection, only("a"), toggle("a"));
    expect(s.selected).toEqual({ kind: "none" });
    expect(s.reading).toBe(false);
  });

  test("toggle with nothing selected selects that one", () => {
    expect(run(emptySelection, toggle("b")).selected).toEqual({ kind: "one", key: "b" });
  });

  test("range selects from the anchor, which stays", () => {
    let s = run(emptySelection, only("b"), range("d"));
    expect(keys(s)).toEqual(["b", "c", "d"]);
    expect(cursorOf(s.selected)).toBe("d");
    expect(s.anchor).toBe("b");
    s = run(s, range("a"));
    expect(keys(s)).toEqual(["a", "b"]);
  });

  test("range without an anchor starts at the message selected and anchors there", () => {
    const s = run({ selected: { kind: "one", key: "c" }, anchor: "", reading: true }, range("e"));
    expect(keys(s)).toEqual(["c", "d", "e"]);
    expect(s.anchor).toBe("c");
  });

  test("range from an anchor no longer listed starts at the message", () => {
    const s = run({ selected: { kind: "none" }, anchor: "gone", reading: false }, range("c"));
    expect(s.selected).toEqual({ kind: "one", key: "c" });
    expect(s.anchor).toBe("c");
  });

  test("several leave a narrow reading pane, and keep a wide one's", () => {
    expect(run(emptySelection, only("a", false), range("b", false)).reading).toBe(false);
    expect(run(emptySelection, only("a"), range("b")).reading).toBe(true);
  });
});

describe("selectAll", () => {
  test("selects every message, keeping the cursor", () => {
    const s = run(emptySelection, only("b"), { type: "selectAll", order, wide: true });
    expect(s.selected).toEqual({ kind: "many", keys: new Set(order), cursor: "b" });
    expect(s.reading).toBe(true);
    expect(run(emptySelection, only("b", false), { type: "selectAll", order, wide: false }).reading).toBe(false);
  });

  test("with nothing selected has no cursor", () => {
    expect(cursorOf(run(emptySelection, { type: "selectAll", order, wide: true }).selected)).toBe("");
  });

  test("of one message does nothing", () => {
    const s = run(emptySelection, only("a"));
    expect(reduceSelection(s, { type: "selectAll", order: ["a"], wide: true })).toBe(s);
  });
});

describe("escape", () => {
  test("ends several first, keeping the cursor selected", () => {
    const s = run(many(["a", "b", "c"], "a"), { type: "escape", wide: true });
    expect(s.selected).toEqual({ kind: "one", key: "c" });
  });

  test("then leaves a narrow reading pane", () => {
    const s = run(emptySelection, only("b", false), { type: "escape", wide: false });
    expect(s.selected).toEqual({ kind: "one", key: "b" });
    expect(s.reading).toBe(false);
  });

  test("then selects nothing", () => {
    expect(run(emptySelection, only("b"), { type: "escape", wide: true })).toEqual({ selected: { kind: "none" }, anchor: "b", reading: false });
  });

  test("with nothing selected changes nothing", () => {
    expect(reduceSelection(emptySelection, { type: "escape", wide: true })).toBe(emptySelection);
  });
});

describe("clear and back", () => {
  test("clear ends several only", () => {
    expect(run(many(["a", "b"], "a"), { type: "clear" }).selected).toEqual({ kind: "one", key: "b" });
    const one = run(emptySelection, only("a"));
    expect(reduceSelection(one, { type: "clear" })).toBe(one);
  });

  test("back leaves the reading pane, keeping the selection", () => {
    const s = run(emptySelection, only("a", false), { type: "back" });
    expect(s).toEqual({ selected: { kind: "one", key: "a" }, anchor: "a", reading: false });
    expect(reduceSelection(s, { type: "back" })).toBe(s);
  });
});

describe("notification", () => {
  test("ends several and the anchor, then shows its message alone", () => {
    let s = run(many(["a", "b"], "a"), { type: "notified" });
    expect(s.selected).toEqual({ kind: "one", key: "b" });
    expect(s.anchor).toBe("");
    s = run(s, { type: "show", key: "d" });
    expect(s).toEqual({ selected: { kind: "one", key: "d" }, anchor: "", reading: true });
    // A Shift selection then starts at it.
    expect(keys(run(s, range("e")))).toEqual(["d", "e"]);
  });
});

describe("prune", () => {
  const present = (...k: string[]) => ({ type: "prune", present: new Set(k) }) as const;

  test("keeps a selection whose messages are all there", () => {
    const s = many(["a", "b"], "a");
    expect(reduceSelection(s, present(...order))).toBe(s);
    expect(reduceSelection(emptySelection, present())).toBe(emptySelection);
  });

  test("a selected message that went away selects nothing", () => {
    const s = run(emptySelection, only("b"), present("a", "c"));
    expect(s.selected).toEqual({ kind: "none" });
    expect(s.reading).toBe(false);
  });

  test("several keep those still there, with the cursor on one of them", () => {
    let s = run(many(["a", "b", "c"], "a"), present("a", "b", "d"));
    expect(s.selected).toEqual({ kind: "many", keys: new Set(["a", "b"]), cursor: "a" });
    expect(s.reading).toBe(false);
    s = run(many(["a", "b", "c"], "a"), present("b", "c"));
    expect(s.selected).toEqual({ kind: "many", keys: new Set(["b", "c"]), cursor: "c" });
    expect(s.reading).toBe(true);
  });

  test("a cursor that went away moves to the first one still selected", () => {
    const s = run(many(["a", "b", "c"], "a"), toggle("d"), toggle("d"), present("a", "b", "c"));
    expect(s.selected).toEqual({ kind: "many", keys: new Set(["a", "b", "c"]), cursor: "a" });
    expect(s.reading).toBe(false);
  });

  test("no cursor moves to the first one still selected", () => {
    const s = run(emptySelection, { type: "selectAll", order, wide: true }, present("b", "c"));
    expect(s.selected).toEqual({ kind: "many", keys: new Set(["b", "c"]), cursor: "b" });
  });

  test("one left is simply selected", () => {
    const s = run(many(["a", "b"], "a"), present("a"));
    expect(s.selected).toEqual({ kind: "one", key: "a" });
  });

  test("none left leaves the cursor selected when it is still there", () => {
    const s = run(many(["a", "b", "c"], "a"), toggle("e"), toggle("e"), toggle("d"), toggle("d"), present("d", "e"));
    expect(s.selected).toEqual({ kind: "one", key: "d" });
    expect(run(many(["a", "b"], "a"), present("c")).selected).toEqual({ kind: "none" });
  });
});

describe("deleteMany", () => {
  test("moves to the message after the selection", () => {
    const s = run(many(["b", "c"], "b"), { type: "deleteMany", keys: new Set(["b", "c"]), order });
    expect(s.selected).toEqual({ kind: "one", key: "d" });
    expect(s.reading).toBe(true);
  });

  test("or else before it", () => {
    const s = run(many(["d", "e"], "d"), { type: "deleteMany", keys: new Set(["d", "e"]), order });
    expect(s.selected).toEqual({ kind: "one", key: "c" });
  });

  test("or else to nothing", () => {
    const s = run(emptySelection, only("a"), { type: "selectAll", order, wide: true }, { type: "deleteMany", keys: new Set(order), order });
    expect(s.selected).toEqual({ kind: "none" });
    expect(s.reading).toBe(false);
  });
});

describe("deleteOne and restore", () => {
  test("moves the cursor on it to the next message, or else the one before", () => {
    expect(run(emptySelection, only("b"), { type: "deleteOne", key: "b", order }).selected).toEqual({ kind: "one", key: "c" });
    expect(run(emptySelection, only("e"), { type: "deleteOne", key: "e", order }).selected).toEqual({ kind: "one", key: "d" });
    const last = run(emptySelection, only("a"), { type: "deleteOne", key: "a", order: ["a"] });
    expect(last.selected).toEqual({ kind: "none" });
    expect(last.reading).toBe(false);
  });

  test("leaves another selection alone", () => {
    const s = run(emptySelection, only("b"));
    expect(reduceSelection(s, { type: "deleteOne", key: "c", order })).toBe(s);
  });

  test("moves the cursor among several", () => {
    const s = run(many(["a", "b", "c"], "a"), toggle("c"), { type: "deleteOne", key: "c", order });
    expect(s.selected).toEqual({ kind: "many", keys: new Set(["a", "b"]), cursor: "d" });
  });

  test("restore selects the message again while the cursor is where the delete put it", () => {
    let s = run(emptySelection, only("b"), { type: "deleteOne", key: "b", order }, { type: "restore", key: "b", from: "c" });
    expect(s.selected).toEqual({ kind: "one", key: "b" });
    s = run(emptySelection, only("a"), { type: "deleteOne", key: "a", order: ["a"] }, { type: "restore", key: "a", from: "" });
    expect(s.selected).toEqual({ kind: "one", key: "a" });
    s = run(emptySelection, only("b"), { type: "deleteOne", key: "b", order }, only("e"), { type: "restore", key: "b", from: "c" });
    expect(s.selected).toEqual({ kind: "one", key: "e" });
  });
});
