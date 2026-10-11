// Browser regression test for bounded pagination and read-error feedback.
// Uses the real preview UI with a deterministic 350-message HTTP fixture.
import assert from "node:assert/strict";
import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync, unlinkSync, rmdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";

const root = join(dirname(fileURLToPath(import.meta.url)), "../..");
const temp = mkdtempSync(join(tmpdir(), "gotify-messages-test-"));
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
  let navigation: { serverId: number; appId: number; messageId: number } | null = null;
  // The test fires the page's events, and can make the route follow a hash change late, as a busy page does.
  await page.route("**/__mygo.js", async (route) => {
    const res = await route.fetch();
    await route.fulfill({ response: res, body: (await res.text()).replace("const listeners = new Map();", "const listeners = window.__listeners = new Map();") });
  });
  await page.addInitScript(() => {
    const add = window.addEventListener;
    window.addEventListener = function (type: string, fn: any, opts?: any) {
      if (type !== "hashchange") return add.call(this, type, fn, opts);
      return add.call(this, type, (e: Event) => setTimeout(() => fn(e), (window as any).__hashDelay ?? 0), opts);
    } as typeof window.addEventListener;
  });
  const queries: { limit: number; before: { id: number } | null }[] = [];
  const messages = Array.from({ length: 350 }, (_, i) => ({
    serverId: 1, appId: 1, id: 350 - i, title: `Fixture ${350 - i}`, body: "Pagination regression message",
    date: new Date(Date.UTC(2026, 0, 1, 0, 0, 350 - i)).toISOString(), priority: 5, read: true, markdown: false,
    clickUrl: "", imageUrl: "", imageSrc: "",
  }));
  await page.route("**/__call", async (route) => {
    const { method, args } = route.request().postDataJSON();
    if (method === "Desktop.Messages") {
      const q = args[0]; queries.push(q);
      const matching = messages.filter((m) => (!q.before || m.id < q.before.id) && (!q.include || m.id <= q.include.id) && (!q.search || m.title.includes(q.search)));
      const rows = matching.slice(0, q.limit);
      const last = rows.at(-1);
      // The page shows "older messages" only when the server says newer ones exist.
      const hasNewer = !!q.include && messages.some((m) => m.id > q.include.id);
      await route.fulfill({ json: { result: { messages: rows, hasMore: matching.length > rows.length,
        next: matching.length > rows.length && last ? { date: last.date, id: last.id, serverId: last.serverId } : null,
        hasNewer, msgGen: 1 } } });
    } else if (method === "Desktop.TakeNavigation" && navigation) {
      const result = navigation;
      navigation = null;
      await route.fulfill({ json: { result } });
    } else if (method === "Desktop.MarkAllRead") {
      await route.fulfill({ status: 500, json: { error: "Read operation failed: disk full" } });
    } else await route.continue();
  });
  const fixture = (id: number) => page.getByText(`Fixture ${id}`, { exact: true }).first();
  await page.goto(`${url}/#/`);
  await fixture(350).waitFor();
  assert.equal(queries[0]?.limit, 100);
  for (let pageNumber = 1; pageNumber <= 3; pageNumber++) {
    const before = queries.length;
    await page.locator("[data-key]").first().evaluate((el) => {
      const pane = el.closest(".msg-list-scroll");
      if (!(pane instanceof HTMLElement)) throw new Error("No list scroller");
      pane.scrollTop = pane.scrollHeight;
    });
    await page.waitForFunction((previous) => document.querySelectorAll("[data-key]").length > 0 && document.querySelector(`[data-key="1/${previous}"]`) !== null, 350 - pageNumber * 100 + 1);
    // Loading starts close to the end; let the request complete before scrolling again.
    for (let i = 0; queries.length === before && i < 50; i++) await page.waitForTimeout(20);
    assert.ok(queries.length > before, "scroll did not request the next page");
  }
  assert.ok(queries.slice(1).some((q) => q.before !== null), "pagination reread the entire prefix");
  assert.ok(queries.every((q) => q.limit === 100), "page size grew without a bound");
  await page.getByRole("button", { name: /Mark all.*read/i }).click();
  await page.getByText("Read operation failed: disk full", { exact: false }).first().waitFor();
  navigation = { serverId: 1, appId: 1, messageId: 150 };
  await page.reload();
  await fixture(150).waitFor();
  await page.getByRole("button", { name: "Show latest messages" }).click();
  await fixture(350).waitFor();
  // A notification clicked on all messages opens its app's page with the message selected, even when
  // the page being left loads it before the route changes.
  await page.evaluate(() => (location.hash = "#/"));
  await fixture(350).waitFor();
  await page.evaluate(() => ((window as any).__hashDelay = 300));
  navigation = { serverId: 1, appId: 1, messageId: 200 };
  const sent = queries.length;
  await page.evaluate(() => (window as any).__listeners.get("navigate").forEach((fn: (p: unknown) => void) => fn(null)));
  // The page left behind may show the message for a moment; the app's page must keep it.
  for (let i = 0; !queries.slice(sent).some((q: any) => q.appId === 1) && i < 100; i++) await page.waitForTimeout(20);
  await page.locator(".msg-read-pane").getByText("Fixture 200", { exact: true }).waitFor({ timeout: 3000 });
  assert.equal(await page.evaluate(() => location.hash), "#/s/1/a/1");
  assert.deepEqual(errors, []);
  console.log("PASS: cursor pagination, historical navigation, return to latest, notification navigation, and visible read failure");
} finally {
  await browser?.close();
  preview.kill();
  await exited;
  unlinkSync(binary);
  rmdirSync(temp);
}
