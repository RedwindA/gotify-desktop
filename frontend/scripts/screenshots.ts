// Renders the main views, light and dark, into dist/screenshots at the repository
// root. It serves the built frontend with demo data through cmd/preview and
// drives Chrome (CHROME, else the installed google-chrome) with playwright-core.
import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";
import { chromium, type Browser, type Page } from "playwright-core";

const root = join(dirname(fileURLToPath(import.meta.url)), "../..");
const out = join(root, "dist/screenshots");
mkdirSync(out, { recursive: true });

// The preview is built once and run directly, so that killing it stops the server itself.
const bin = join(mkdtempSync(join(tmpdir(), "gotify-preview-")), "preview");
execFileSync("go", ["build", "-o", bin, "./cmd/preview"], { cwd: root, stdio: "inherit" });

const procs: ChildProcess[] = [];
async function preview(...args: string[]): Promise<string> {
  const proc = spawn(bin, ["-addr", "127.0.0.1:0", ...args], { cwd: root, stdio: ["ignore", "pipe", "inherit"] });
  procs.push(proc);
  return new Promise((resolve, reject) => {
    createInterface({ input: proc.stdout! }).once("line", resolve);
    proc.once("exit", (code) => reject(new Error(`preview exited with ${code}`)));
  });
}

type Shot = { name: string; base: string; hash?: string; width?: number; height?: number; steps?: (p: Page) => Promise<void> };
let browser: Browser | undefined;
try {
  const demo = await preview();
  const empty = await preview("-empty");
  const demoZh = await preview("-lang", "zh-CN");
  const emptyZh = await preview("-empty", "-lang", "zh-CN");
  browser = await chromium.launch({ executablePath: process.env.CHROME ?? "/usr/bin/google-chrome" });
  const en = { add: "Add server", address: "Server address", user: "Username", connect: "Connect", advanced: "Advanced", tls: "Skip TLS certificate verification" };
  const zh = { add: "添加服务器", address: "服务器地址", user: "用户名", connect: "连接", advanced: "高级", tls: "跳过 TLS 证书验证" };
  const addServer = (l: typeof en) => async (p: Page) => {
    await p.getByRole("button", { name: l.add }).first().click();
    await p.getByLabel(l.address).fill("gotify.example.com");
    await p.getByLabel(l.user).fill("alice");
    await p.getByRole("button", { name: l.connect }).click();
    await p.getByRole("button", { name: l.advanced }).click();
    await p.getByLabel(l.tls).check();
  };
  const shots: Shot[] = [
    { name: "welcome", base: empty, width: 1000, height: 640 },
    { name: "main", base: demo },
    { name: "app-scope", base: demo, hash: "#/s/1/a/1" },
    {
      name: "server-menu",
      base: demo,
      hash: "#/s/1",
      steps: async (p) => {
        await p.getByRole("button", { name: "Home Server options" }).click();
      },
    },
    { name: "add-server", base: empty, width: 1000, height: 760, steps: addServer(en) },
    { name: "settings", base: demo, hash: "#/settings", height: 900 },
    { name: "main-zh", base: demoZh },
    { name: "add-server-zh", base: emptyZh, width: 1000, height: 760, steps: addServer(zh) },
    { name: "settings-zh", base: demoZh, hash: "#/settings", height: 900 },
  ];

  for (const theme of ["light", "dark"]) {
    for (const s of shots) {
      const page = await browser.newPage({ viewport: { width: s.width ?? 1100, height: s.height ?? 760 }, colorScheme: theme as "light" | "dark" });
      page.setDefaultTimeout(10_000);
      page.on("pageerror", (err) => console.error(`${s.name}: ${err.message}`));
      page.on("console", (m) => m.type() === "error" && console.error(`${s.name}: ${m.text()}`));
      // The events stream keeps the network busy, so wait for the page to render instead.
      await page.goto(`${s.base}/?theme=${theme}${s.hash ?? "#/"}`);
      await page.locator("nav").first().waitFor();
      await s.steps?.(page);
      await page.waitForTimeout(500);
      const path = join(out, `${s.name}-${theme}.png`);
      await page.screenshot({ path });
      console.log(path);
      await page.close();
    }
  }
} finally {
  await browser?.close();
  for (const p of procs) p.kill();
  rmSync(dirname(bin), { recursive: true, force: true });
}
