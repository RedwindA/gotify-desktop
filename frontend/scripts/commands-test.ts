// Browser regression test for the app menu's commands and their shortcuts:
// one that arrives before the page listens, settings behind a dialog, adding
// a server held while another dialog is open, and not held by the settings of
// an app that went away. Uses the real preview UI.
import assert from "node:assert/strict";
import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync, unlinkSync, rmdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";

const root = join(dirname(fileURLToPath(import.meta.url)), "../..");
const temp = mkdtempSync(join(tmpdir(), "gotify-commands-test-"));
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
  let command: string | null = "addServer";
  let state: any;
  await page.route("**/__call", async (route) => {
    const { method } = route.request().postDataJSON();
    if (method === "Desktop.TakeCommand") {
      const result = command;
      command = null;
      await route.fulfill({ json: { result } });
    } else if (method === "Desktop.State") {
      const res = await route.fetch();
      state = (await res.json()).result;
      await route.fulfill({ response: res, json: { result: state } });
    } else await route.continue();
  });
  // Asks the page to run a command, as the app's menu does.
  const send = async (c: string) => {
    command = c;
    await page.evaluate(() => (window as any).__listeners.get("command").forEach((fn: (p: unknown) => void) => fn(null)));
    for (let i = 0; command !== null && i < 100; i++) await page.waitForTimeout(20);
    assert.equal(command, null, "the page did not take the command");
  };
  const addDialog = page.getByRole("dialog").filter({ hasText: "Add a server" });
  const prefsDialog = page.getByRole("dialog").filter({ hasText: "Notifications from this app" });
  const openPrefs = async () => {
    await page.getByRole("button", { name: "Backup options" }).first().click();
    await page.getByRole("menuitem", { name: "Notification settings…" }).click();
    await prefsDialog.waitFor();
  };
  const settle = () => page.waitForTimeout(300);
  const mod = process.platform === "darwin" ? "Meta" : "Control";

  // A command from before the page listened is carried out once it does.
  await page.goto(`${url}/#/`);
  await addDialog.waitFor();
  // Another while the add-server dialog is open adds nothing.
  await send("addServer");
  await settle();
  assert.equal(await addDialog.count(), 1);
  await page.keyboard.press("Escape");
  await addDialog.waitFor({ state: "detached" });

  // Behind another dialog, settings opens at once and adding a server waits for it to close.
  await page.locator("[data-key]").first().waitFor();
  await openPrefs();
  await send("addServer");
  await send("settings");
  await page.waitForFunction(() => location.hash === "#/settings");
  await settle();
  assert.equal(await addDialog.count(), 0);
  assert.equal(await prefsDialog.count(), 1);
  await page.keyboard.press("Escape");
  await prefsDialog.waitFor({ state: "detached" });
  await addDialog.waitFor();
  await page.keyboard.press("Escape");
  await addDialog.waitFor({ state: "detached" });

  // The shortcuts follow the same rules.
  await page.evaluate(() => (location.hash = "#/"));
  await page.locator("[data-key]").first().waitFor();
  await openPrefs();
  await page.keyboard.press(`${mod}+n`);
  await page.keyboard.press(`${mod}+,`);
  await page.waitForFunction(() => location.hash === "#/settings");
  await settle();
  assert.equal(await addDialog.count(), 0);
  await page.keyboard.press("Escape");
  await prefsDialog.waitFor({ state: "detached" });
  await addDialog.waitFor();
  await page.keyboard.press(`${mod}+n`);
  await settle();
  assert.equal(await addDialog.count(), 1);
  await page.keyboard.press("Escape");
  await addDialog.waitFor({ state: "detached" });

  // An app that goes away closes its notification settings, and adding a server no longer waits.
  await page.evaluate(() => (location.hash = "#/"));
  await page.locator("[data-key]").first().waitFor();
  await openPrefs();
  const gone = structuredClone(state);
  gone.gen = Number.MAX_SAFE_INTEGER;
  gone.servers[0].apps = gone.servers[0].apps.filter((a: { name: string }) => a.name !== "Backup");
  await page.evaluate((s) => (window as any).__listeners.get("state").forEach((fn: (p: unknown) => void) => fn(s)), gone);
  await prefsDialog.waitFor({ state: "detached" });
  await page.keyboard.press(`${mod}+n`);
  await addDialog.waitFor();
  await page.keyboard.press("Escape");
  await addDialog.waitFor({ state: "detached" });

  assert.deepEqual(errors, []);
  console.log("PASS: command before the page listened, settings behind a dialog, add server held until the dialog closes and dropped while it shows, shortcuts, an app gone from behind its settings");
} finally {
  await browser?.close();
  preview.kill();
  await exited;
  unlinkSync(binary);
  rmdirSync(temp);
}
