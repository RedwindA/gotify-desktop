// Browser regression test for selecting several messages, their bulk actions,
// deleting all messages of an app and the history setting. Uses the real
// preview UI with a 20-message fixture and records the calls that change data.
import assert from "node:assert/strict";
import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync, unlinkSync, rmdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";

const root = join(dirname(fileURLToPath(import.meta.url)), "../..");
const temp = mkdtempSync(join(tmpdir(), "gotify-selection-test-"));
const binary = join(temp, process.platform === "win32" ? "preview.exe" : "preview");
execFileSync("go", ["build", "-o", binary, "./cmd/preview"], { cwd: root });
const preview = spawn(binary, ["-addr", "127.0.0.1:0"], { cwd: root, stdio: ["ignore", "pipe", "inherit"], windowsHide: true });
const exited = new Promise<void>((resolve) => preview.once("exit", () => resolve()));
let browser;
try {
  const url = await new Promise<string>((resolve, reject) => {
    createInterface({ input: preview.stdout! }).once("line", resolve);
    preview.once("exit", () => reject(new Error("preview exited before listening")));
  });
  browser = await chromium.launch({ executablePath: process.env.CHROME ?? (process.platform === "win32" ? "C:/Program Files/Google/Chrome/Application/chrome.exe" : "/usr/bin/google-chrome") });
  const page = await browser.newPage({ viewport: { width: 1400, height: 800 } });
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  // The test fires the page's events.
  await page.route("**/__mygo.js", async (route) => {
    const res = await route.fetch();
    await route.fulfill({ response: res, body: (await res.text()).replace("const listeners = new Map();", "const listeners = window.__listeners = new Map();") });
  });
  let navigation: { serverId: number; appId: number; messageId: number } | null = null;
  let messages = Array.from({ length: 20 }, (_, i) => ({
    serverId: 1, appId: 1, id: 20 - i, title: `Fixture ${20 - i}`, body: "Selection regression message",
    date: new Date(Date.UTC(2026, 0, 1, 0, 0, 20 - i)).toISOString(), priority: 5, read: false, markdown: false,
    clickUrl: "", imageUrl: "", imageSrc: "",
  }));
  const calls: { method: string; args: any[] }[] = [];
  const recorded = new Set(["Desktop.MarkRead", "Desktop.DeleteMessages", "Desktop.ClearMessages"]);
  await page.route("**/__call", async (route) => {
    const { method, args } = route.request().postDataJSON();
    if (method === "Desktop.Messages") {
      await route.fulfill({ json: { result: { messages, hasMore: false, next: null, hasNewer: false, msgGen: 1 } } });
    } else if (method === "Desktop.TakeNavigation" && navigation) {
      const result = navigation;
      navigation = null;
      await route.fulfill({ json: { result } });
    } else if (recorded.has(method)) {
      calls.push({ method, args });
      await route.fulfill({ json: { result: null } });
    } else {
      if (method === "Desktop.SetSettings") calls.push({ method, args });
      await route.continue();
    }
  });
  const row = (id: number) => page.locator(`[data-key="1/${id}"]`);
  const readPane = page.locator(".msg-read-pane");
  const picked = () => page.locator('[role="option"][aria-selected="true"]').count();
  const callsOf = (method: string) => calls.filter((c) => c.method === method);
  const mod = process.platform === "darwin" ? "Meta" : "Control";
  // Tells the page that messages changed, as Go does after a sync or a read.
  let msgGen = 1;
  const messagesChanged = () =>
    page.evaluate(async (gen) => {
      const state = await (window as any).mygo.call("Desktop.State");
      (window as any).__listeners.get("state").forEach((fn: (s: unknown) => void) => fn({ ...state, gen: state.gen + 1000 + gen, msgGen: gen }));
    }, ++msgGen);

  await page.goto(`${url}/#/`);
  await row(20).waitFor();
  await row(20).click();
  await readPane.getByText("Fixture 20", { exact: true }).waitFor();

  // Ctrl-click adds a message to the one selected; Shift-click selects from the last one clicked.
  await row(18).click({ modifiers: [mod] });
  await readPane.getByText("2 messages selected").waitFor();
  await row(15).click({ modifiers: ["Shift"] });
  await readPane.getByText("4 messages selected").waitFor();
  assert.equal(await picked(), 4);
  // Ctrl-click removes one again.
  await row(16).click({ modifiers: [mod] });
  await readPane.getByText("3 messages selected").waitFor();
  await row(16).click({ modifiers: [mod] });
  await readPane.getByText("4 messages selected").waitFor();

  // A selected row's menu acts on all of them.
  const readBefore = callsOf("Desktop.MarkRead").length;
  await row(16).click({ button: "right" });
  await page.getByRole("menuitem", { name: "Mark 4 messages as read" }).click();
  await page.waitForFunction(() => document.querySelectorAll('[role="option"][aria-selected="true"] [data-unread]').length === 0);
  const read = callsOf("Desktop.MarkRead").slice(readBefore);
  assert.deepEqual(read.map((c) => [c.args[0], [...c.args[1]].sort((a: number, b: number) => a - b)]), [[1, [15, 16, 17, 18]]]);

  // Delete asks first; cancelling deletes nothing.
  await row(16).focus();
  await page.keyboard.press("Delete");
  const dialog = page.getByRole("alertdialog");
  await dialog.getByText("Delete 4 messages?").waitFor();
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await dialog.waitFor({ state: "detached" });
  assert.equal(callsOf("Desktop.DeleteMessages").length, 0);
  await readPane.getByRole("button", { name: "Delete" }).click();
  await dialog.getByRole("button", { name: "Delete" }).click();
  for (let i = 0; callsOf("Desktop.DeleteMessages").length === 0 && i < 100; i++) await page.waitForTimeout(20);
  const deleted = callsOf("Desktop.DeleteMessages")[0]!.args[0].map((r: { serverId: number; id: number }) => r.id).sort((a: number, b: number) => a - b);
  assert.deepEqual(deleted, [15, 16, 17, 18]);
  // The message after the selection is selected next.
  await readPane.getByText("Fixture 14", { exact: true }).waitFor();

  // Shift+arrows extend the selection, Escape ends it, Ctrl+A selects every message.
  await row(10).click();
  await page.keyboard.press("Shift+ArrowDown");
  await page.keyboard.press("Shift+ArrowDown");
  await readPane.getByText("3 messages selected").waitFor();
  await page.keyboard.press("Escape");
  await readPane.getByText("Fixture 8", { exact: true }).waitFor();
  assert.equal(await picked(), 1);
  await page.keyboard.press(`${mod}+a`);
  await readPane.getByText("20 messages selected").waitFor();
  await readPane.getByRole("button", { name: "Clear selection" }).click();
  await readPane.getByText("Fixture 8", { exact: true }).waitFor();

  // A message removed elsewhere leaves the selection; one left is simply selected.
  await row(12).click();
  await row(11).click({ modifiers: [mod] });
  await readPane.getByText("2 messages selected").waitFor();
  messages = messages.filter((m) => m.id !== 11);
  await messagesChanged();
  await readPane.getByText("Fixture 12", { exact: true }).waitFor();
  assert.equal(await picked(), 1);

  // A refresh while the question is open does not lose where the selection was.
  await row(3).click();
  await row(2).click({ modifiers: [mod] });
  await page.keyboard.press("Delete");
  await dialog.getByText("Delete 2 messages?").waitFor();
  messages = messages.map((m) => ({ ...m, read: true }));
  await messagesChanged();
  await page.waitForTimeout(200);
  await dialog.getByRole("button", { name: "Delete" }).click();
  await readPane.getByText("Fixture 1", { exact: true }).waitFor();

  // A notification's message shows on its own, also on the page of its app.
  await page.evaluate(() => (location.hash = "#/s/1/a/1"));
  await row(8).click();
  await row(7).click({ modifiers: [mod] });
  await readPane.getByText("2 messages selected").waitFor();
  navigation = { serverId: 1, appId: 1, messageId: 9 };
  await page.evaluate(() => (window as any).__listeners.get("navigate").forEach((fn: (p: unknown) => void) => fn(null)));
  await readPane.getByText("Fixture 9", { exact: true }).waitFor();
  assert.equal(await picked(), 1);
  // Shift+arrows from there stay anchored at the notification's message.
  await row(9).focus();
  await page.keyboard.press("Shift+ArrowDown");
  await page.keyboard.press("Shift+ArrowDown");
  await readPane.getByText("3 messages selected").waitFor();
  assert.deepEqual(
    await page.locator('[role="option"][aria-selected="true"]').evaluateAll((els) => els.map((el) => el.getAttribute("data-key"))),
    ["1/9", "1/8", "1/7"],
  );
  await page.keyboard.press("Escape");
  await page.evaluate(() => (location.hash = "#/"));
  await row(20).waitFor();

  // A selection made in a wide window stays on the list when the window narrows,
  // where its actions show in a bar over the list.
  await row(6).click();
  await row(5).click({ modifiers: [mod] });
  await readPane.getByText("2 messages selected").waitFor();
  await page.setViewportSize({ width: 600, height: 800 });
  await page.locator(".msg-selection-bar").getByText("2 messages selected").waitFor();
  await row(4).click({ modifiers: [mod] });
  await page.locator(".msg-selection-bar").getByText("3 messages selected").waitFor();
  await page.keyboard.press("Escape");
  await page.locator(".msg-selection-bar").waitFor({ state: "detached" });
  await page.setViewportSize({ width: 1400, height: 800 });

  // Deleting all messages of an app asks first.
  await page.getByRole("button", { name: "Backup options" }).first().click();
  await page.getByRole("menuitem", { name: "Delete all messages…" }).click();
  await dialog.getByText("Delete all messages of “Backup”?").waitFor();
  await dialog.getByRole("button", { name: "Delete all" }).click();
  for (let i = 0; callsOf("Desktop.ClearMessages").length === 0 && i < 100; i++) await page.waitForTimeout(20);
  assert.deepEqual(callsOf("Desktop.ClearMessages")[0]!.args, [1, 1]);

  // History keeps messages for the chosen time.
  await page.evaluate(() => (location.hash = "#/settings"));
  await page.getByRole("combobox", { name: "Keep messages" }).click();
  await page.getByRole("option", { name: "1 month" }).click();
  for (let i = 0; !callsOf("Desktop.SetSettings").some((c) => c.args[1].keepDays === 30) && i < 100; i++) await page.waitForTimeout(20);
  assert.ok(callsOf("Desktop.SetSettings").some((c) => c.args[1].keepDays === 30), "keepDays not saved");

  assert.deepEqual(errors, []);
  console.log("PASS: Ctrl/Shift selection, bulk menu, bulk read and delete with confirmation, keyboard selection, removal and refresh during a selection, notification navigation, narrow bar, delete all of an app, keep messages");
} finally {
  await browser?.close();
  preview.kill();
  await exited;
  unlinkSync(binary);
  rmdirSync(temp);
}
