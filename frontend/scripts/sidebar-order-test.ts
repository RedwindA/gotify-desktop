// Drags apps in the sidebar and moves them from their menu, against the
// production preview over demo data, in Chrome through playwright-core.
import assert from "node:assert/strict";
import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";

const root = join(dirname(fileURLToPath(import.meta.url)), "../..");
const temp = mkdtempSync(join(tmpdir(), "gotify-sidebar-order-test-"));
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
  browser = await chromium.launch({
    executablePath: process.env.CHROME ?? (process.platform === "win32" ? "C:/Program Files/Google/Chrome/Application/chrome.exe" : "/usr/bin/google-chrome"),
  });
  const page = await browser.newPage({ viewport: { width: 1400, height: 800 } });
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(url);
  await page.locator(".sidenav-app").first().waitFor();

  // An app's name, from the label of its menu.
  const names = () =>
    [...document.querySelectorAll(".sidenav-app")].map((el) => el.querySelector("[aria-label$=' options']")?.getAttribute("aria-label")?.replace(/ options$/, ""));
  const order = () => page.evaluate(names);
  const row = (name: string) => page.locator(".sidenav-app").filter({ hasText: name });
  const settled = async (want: string[]) => {
    await page.waitForFunction(`JSON.stringify((${names})()) === ${JSON.stringify(JSON.stringify(want))}`);
    assert.deepEqual(
      await page.evaluate(async () => {
        const s = await (window as any).mygo.call("Desktop.State");
        return s.servers.flatMap((sv: any) => sv.apps.map((a: any) => a.name));
      }),
      want,
      "the backend's order",
    );
  };
  assert.deepEqual(await order(), ["Backup", "Monitoring", "Home Assistant", "CI", "Deploys"]);

  // Drag Home Assistant above Backup.
  const from = (await row("Home Assistant").boundingBox())!;
  const to = (await row("Backup").boundingBox())!;
  await page.mouse.move(from.x + 40, from.y + from.height / 2);
  await page.mouse.down();
  await page.mouse.move(from.x + 40, from.y + from.height / 2 - 10, { steps: 3 });
  await page.mouse.move(from.x + 40, to.y + 4, { steps: 10 });
  await page.mouse.up();
  await settled(["Home Assistant", "Backup", "Monitoring", "CI", "Deploys"]);
  assert.ok(!page.url().includes("/a/3"), "the drag must not open the app");

  // A click still opens it.
  await row("Monitoring").getByText("Monitoring", { exact: true }).click();
  await page.waitForURL(/#\/s\/1\/a\/2$/);

  // Move down from the app's menu.
  await row("CI").getByRole("button", { name: "CI options" }).click();
  await page.getByRole("menuitem", { name: "Move down" }).click();
  await settled(["Home Assistant", "Backup", "Monitoring", "Deploys", "CI"]);
  await row("CI").getByRole("button", { name: "CI options" }).click();
  assert.equal(await page.getByRole("menuitem", { name: "Move down" }).getAttribute("aria-disabled"), "true", "the last app can't move down");
  await page.keyboard.press("Escape");

  // Drag does not cross servers: Deploys stays in Work Gotify.
  const deploys = (await row("Deploys").boundingBox())!;
  const top = (await row("Home Assistant").boundingBox())!;
  await page.mouse.move(deploys.x + 40, deploys.y + deploys.height / 2);
  await page.mouse.down();
  await page.mouse.move(deploys.x + 40, deploys.y - 10, { steps: 3 });
  await page.mouse.move(deploys.x + 40, top.y + 4, { steps: 10 });
  await page.mouse.up();
  await settled(["Home Assistant", "Backup", "Monitoring", "Deploys", "CI"]);

  // A finger drags after holding still; a swipe leaves the order alone.
  const touch = await (await browser.newContext({ viewport: { width: 1400, height: 800 }, hasTouch: true })).newPage();
  touch.on("pageerror", (error) => errors.push(error.message));
  await touch.goto(url);
  await touch.locator(".sidenav-app").first().waitFor();
  const cdp = await touch.context().newCDPSession(touch);
  const touchRow = (name: string) => touch.locator(".sidenav-app").filter({ hasText: name });
  const drag = async (name: string, to: string, hold: number) => {
    const box = (await touchRow(name).boundingBox())!;
    const x = box.x + 40;
    const y = box.y + box.height / 2;
    await cdp.send("Input.dispatchTouchEvent", { type: "touchStart", touchPoints: [{ x, y }] });
    await touch.waitForTimeout(hold);
    const end = (await touchRow(to).boundingBox())!.y + 4;
    for (let i = 1; i <= 10; i++) {
      await cdp.send("Input.dispatchTouchEvent", { type: "touchMove", touchPoints: [{ x, y: y + ((end - y) * i) / 10 }] });
      await touch.waitForTimeout(16);
    }
    await cdp.send("Input.dispatchTouchEvent", { type: "touchEnd", touchPoints: [] });
  };
  const touchOrder = () => touch.evaluate(names);
  const before = await touchOrder();
  await drag("Backup", "CI", 0);
  await touch.waitForTimeout(300);
  assert.deepEqual(await touchOrder(), before, "a swipe must not reorder");
  await drag("Monitoring", "Backup", 400);
  await touch.waitForFunction(`JSON.stringify((${names})()) === ${JSON.stringify(JSON.stringify(["Home Assistant", "Monitoring", "Backup", "Deploys", "CI"]))}`);

  assert.deepEqual(errors, []);
  console.log("sidebar order: ok");
} finally {
  await browser?.close();
  preview.kill();
  await exited;
  rmSync(temp, { recursive: true, force: true });
}
