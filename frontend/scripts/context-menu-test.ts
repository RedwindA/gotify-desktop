// Run against the production preview, or an isolated demo WebView2 with WEBVIEW_CDP.
import assert from "node:assert/strict";
import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync, unlinkSync, rmdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";

const root = join(dirname(fileURLToPath(import.meta.url)), "../..");
const cdp = process.env.WEBVIEW_CDP;
let preview: ReturnType<typeof spawn> | undefined;
let exited: Promise<void> | undefined;
let temp: string | undefined;
let binary: string | undefined;
let browser;
try {
  let url: string | undefined;
  if (!cdp) {
    temp = mkdtempSync(join(tmpdir(), "gotify-context-menu-test-"));
    binary = join(temp, process.platform === "win32" ? "preview.exe" : "preview");
    execFileSync("go", ["build", "-o", binary, "./cmd/preview"], { cwd: root });
    preview = spawn(binary, ["-addr", "127.0.0.1:0"], { cwd: root, stdio: ["ignore", "pipe", "inherit"], windowsHide: true });
    exited = new Promise<void>((resolve) => preview!.once("exit", () => resolve()));
    url = await new Promise<string>((resolve, reject) => {
      createInterface({ input: preview!.stdout! }).once("line", resolve);
      preview!.once("exit", () => reject(new Error("preview exited before listening")));
    });
  }
  browser = cdp ? await chromium.connectOverCDP(cdp) : await chromium.launch({
    executablePath: process.env.CHROME ?? (process.platform === "win32" ? "C:/Program Files/Google/Chrome/Application/chrome.exe" : "/usr/bin/google-chrome"),
  });
  const page = cdp ? browser.contexts()[0]!.pages()[0]! : await browser.newPage({ viewport: { width: 1400, height: 800 } });
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  if (url) await page.goto(url);
  await page.locator("[data-key]").first().waitFor();
  const wholeBody = await page.evaluate(async () => {
    const result = await (window as any).mygo.call("Desktop.Messages", { limit: 1 });
    return result.messages[0].body as string;
  });
  const readClipboard = () => JSON.parse(execFileSync("powershell.exe", ["-NoProfile", "-Command", "Get-Clipboard -Raw | ConvertTo-Json -Compress"], { encoding: "utf8", windowsHide: true }));
  if (!cdp) await page.evaluate(() => {
    const runtime = (window as any).mygo;
    const original = runtime.call;
    (window as any).__copied = [];
    // The preview has no clipboard. WebView2 instead exercises the native bridge.
    runtime.call = function (method: string, ...args: unknown[]) {
      if (method === "Desktop.CopyText") {
        (window as any).__copied.push(args[0]);
        return Promise.resolve();
      }
      return original.call(this, method, ...args);
    };
    (window as any).__restoreCopy = () => { runtime.call = original; };
  });
  try {
    assert.equal(await page.locator("body").evaluate((el) => !el.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true }))), true, "blank space must suppress the native menu");
    const input = page.locator("input").first();
    assert.equal(await input.evaluate((el) => el.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true }))), true, "text fields must keep their native menu");
    // Cover textarea and nested editable content, including a non-editable island.
    assert.deepEqual(await page.evaluate(() => {
      const area = document.createElement("textarea");
      const editable = document.createElement("div");
      editable.contentEditable = "true";
      editable.innerHTML = '<span>editable</span><span contenteditable="false">static</span>';
      document.body.append(area, editable);
      const allowed = [area, editable.firstElementChild!, editable.lastElementChild!].map(el => el.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
      area.remove(); editable.remove();
      return allowed;
    }), [true, true, false]);
    const cards = page.locator("[data-key]");
    const card = cards.first();
    // The open row menu is the one inside the list. The reading pane has a context menu of its own.
    const list = page.getByRole("listbox");
    const copySelected = list.getByRole("menuitem", { name: /^(Copy selected text|复制选中文字)$/ });
    const copyAll = list.getByRole("menuitem", { name: /^(Copy text|复制文本)$/ });
    await card.click({ button: "right" });
    await copyAll.waitFor();
    assert.equal(await copySelected.count(), 0, "no selection must not offer selected-text copy");
    await page.keyboard.press("Escape");
    const text = card.locator(".selectable").first();
    const expected = await text.evaluate((el) => {
      const node = el.firstChild!;
      const range = document.createRange();
      range.setStart(node, 0);
      range.setEnd(node, Math.min(5, node.textContent!.length));
      const selection = window.getSelection()!;
      selection.removeAllRanges(); selection.addRange(range);
      return selection.toString();
    });
    assert.ok(expected.length > 0);
    await text.click({ button: "right", position: { x: 5, y: 5 } });
    await copySelected.waitFor();
    await page.evaluate(() => window.getSelection()!.removeAllRanges());
    await copySelected.click();
    if (cdp) assert.equal(readClipboard(), expected, "native clipboard must contain the selection snapshot");
    else assert.deepEqual(await page.evaluate(() => (window as any).__copied), [expected], "copy must use the snapshot, even after focus clears the selection");
    // Closing the menu puts the selection back, as WebKit drops it when the menu takes focus.
    // The restore runs in a task queued as the menu closed; this one runs after it.
    await page.evaluate(() => new Promise((resolve) => setTimeout(resolve)));
    assert.equal(await page.evaluate(() => window.getSelection()!.toString()), expected, "closing the menu must restore the selection");
    await page.evaluate(() => window.getSelection()!.removeAllRanges());
    await card.click({ button: "right" });
    await copyAll.waitFor();
    assert.equal(await copySelected.count(), 0, "a reopened menu must not retain stale text");
    await copyAll.click();
    if (cdp) assert.equal(readClipboard(), wholeBody, "native whole-message copy must still work");
    else assert.deepEqual(await page.evaluate(() => (window as any).__copied), [expected, wholeBody], "whole-message copy must still work");
    await text.evaluate((el) => {
      const range = document.createRange(); range.selectNodeContents(el);
      const selection = window.getSelection()!; selection.removeAllRanges(); selection.addRange(range);
    });
    await cards.nth(1).click({ button: "right" });
    await copyAll.waitFor();
    assert.equal(await copySelected.count(), 0, "another card's selection must not leak into this menu");
    await page.keyboard.press("Escape");
    await page.evaluate(() => window.getSelection()!.removeAllRanges());
    await card.click();
    await page.getByRole("button", { name: /^(Copy text|复制文本)$/ }).waitFor();
    assert.equal(await page.getByRole("button", { name: /^(Message actions|消息操作)$/ }).count(), 0, "the reading pane has its actions as buttons, not a More menu");
    await card.focus();
    await page.keyboard.press("Shift+F10");
    await copyAll.waitFor();
    await page.keyboard.press("Escape");
    assert.deepEqual(errors, []);
    console.log(`PASS (${cdp ? "WebView2" : "preview"}): menu suppression, native editing, selection snapshot, stale/other-card selection, whole-message copy, reading-pane actions and Shift+F10`);
  } finally {
    if (!cdp) await page.evaluate(() => { (window as any).__restoreCopy(); delete (window as any).__restoreCopy; delete (window as any).__copied; });
  }
} finally {
  await browser?.close();
  preview?.kill();
  await exited;
  if (binary) unlinkSync(binary);
  if (temp) rmdirSync(temp);
}
