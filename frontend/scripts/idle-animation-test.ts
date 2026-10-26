// Browser regression test: an idle window runs no endless animation. The list
// once kept a spinner below its last row whenever more messages could load, out
// of sight, and the webview recalculated its style every frame (about 9% of a
// core with the window open). Uses the real preview UI with more messages than
// a page holds.
import assert from "node:assert/strict";
import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync, unlinkSync, rmdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";

const root = join(dirname(fileURLToPath(import.meta.url)), "../..");
const temp = mkdtempSync(join(tmpdir(), "gotify-idle-animation-test-"));
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
  // 450 read messages, newest first, served in pages after a cursor.
  const all = Array.from({ length: 450 }, (_, i) => ({
    serverId: 1, appId: 1, id: 450 - i, title: `Fixture ${450 - i}`, body: "Idle animation regression message",
    date: new Date(Date.UTC(2026, 0, 1) + (450 - i) * 1000).toISOString(), priority: 5, read: true, markdown: false,
    clickUrl: "", imageUrl: "", imageSrc: "",
  }));
  let delay = 0;
  await page.route("**/__call", async (route) => {
    const { method, args } = route.request().postDataJSON();
    if (method !== "Desktop.Messages") return route.continue();
    const q = args[0];
    const start = q.before ? all.findIndex((m) => m.id < q.before.id) : 0;
    const messages = all.slice(start, start + q.limit);
    const last = messages.at(-1)!;
    const hasMore = start + q.limit < all.length;
    if (delay) await new Promise((r) => setTimeout(r, delay));
    await route.fulfill({
      json: { result: { messages, hasMore, next: hasMore ? { serverId: 1, id: last.id, date: last.date } : null, hasNewer: false, msgGen: 1 } },
    });
  });
  const endless = () =>
    page.evaluate(() =>
      document.getAnimations().filter((a) => a.playState === "running" && a.effect?.getTiming().iterations === Infinity).map((a) => (a.effect as KeyframeEffect).target?.closest("[role]")?.getAttribute("aria-label") ?? "?"),
    );
  await page.goto(url);
  await page.getByText("Fixture 450", { exact: true }).waitFor();
  await page.waitForTimeout(500);
  assert.deepEqual(await endless(), [], "an endless animation runs with the list at rest and more messages to load");

  // Scrolling to the end shows the spinner while the next page loads, and only then.
  delay = 600;
  const scroller = page.locator(".msg-list-scroll");
  const height = await scroller.evaluate((el) => el.scrollHeight);
  await scroller.evaluate((el) => el.scrollTo({ top: el.scrollHeight }));
  await page.getByRole("status", { name: "Loading" }).waitFor({ timeout: 2000 });
  await page.getByRole("status", { name: "Loading" }).waitFor({ state: "detached", timeout: 2000 });
  assert.ok((await scroller.evaluate((el) => el.scrollHeight)) > height, "the next page did not load");
  await page.waitForTimeout(300);
  assert.deepEqual(await endless(), [], "an endless animation runs after the next page loaded");

  assert.deepEqual(errors, []);
  console.log("PASS: no endless animation at rest, spinner only while the next page loads");
} finally {
  await browser?.close();
  preview.kill();
  await exited;
  unlinkSync(binary);
  rmdirSync(temp);
}
