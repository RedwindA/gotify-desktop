import { describe, expect, test } from "bun:test";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { Markdown } from "@astryxdesign/core/Markdown";
import { InternationalizationProvider } from "@astryxdesign/core/i18n";
import { Theme } from "@astryxdesign/core/theme";
import { neutralTheme } from "@astryxdesign/theme-neutral/built";
import { isRasterDataImage, markdownDataImages } from "../src/dataImages";
import { markdownLinks } from "../src/links";

// A 1×1 PNG; the payload is not checked, so the JPEG case reuses it.
const payload = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";
const png = `data:image/png;base64,${payload}`;
const jpeg = `data:image/jpeg;base64,${payload}`;
const svg = "data:image/svg+xml;base64,PHN2Zy8+";
const html = "data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==";

function render(text: string, plugins = [...markdownLinks, markdownDataImages]): string {
  const markdown = createElement(Markdown, { density: "compact", contentWidth: "100%", plugins }, text);
  const i18n = createElement(InternationalizationProvider, { locale: "en", messages: {} }, markdown);
  return renderToStaticMarkup(createElement(Theme, { theme: neutralTheme, mode: "light" }, i18n));
}

const srcs = (html: string) => [...html.matchAll(/<img[^>]*\ssrc="([^"]*)"/g)].map((m) => m[1]);

describe("isRasterDataImage", () => {
  test("takes base64 raster images in any case", () => {
    for (const type of ["png", "jpeg", "jpg", "gif", "webp", "avif", "bmp"]) expect(isRasterDataImage(`data:image/${type};base64,AAAA`)).toBe(true);
    expect(isRasterDataImage(`DATA:IMAGE/PNG;BASE64,${payload}`)).toBe(true);
  });
  test("leaves SVG, HTML, URL-encoded data and other URLs out", () => {
    for (const url of [svg, html, `data:image/png,${payload}`, "javascript:alert(1)", "https://example.com/a.png"]) expect(isRasterDataImage(url)).toBe(false);
  });
});

describe("Markdown with data: images", () => {
  test("shows the issue's message: text, then an image with no alt", () => {
    const body = `邮件通知 识别文字: 国家任务达成奖励\n\n![](${jpeg})`;
    expect(srcs(render(body))).toEqual([jpeg]);
    expect(srcs(render(body, markdownLinks))).toEqual([]); // what Astryx alone does
  });

  test("finds them inline, in links, quotes, lists and tables", () => {
    const body = [`a ![x](${png}) b`, `[![x](${png})](https://example.com)`, `> ![x](${png})`, `- ![x](${png})`, `| a |\n|---|\n| ![x](${png}) |`].join("\n\n");
    expect(srcs(render(body))).toEqual(Array(5).fill(png));
  });

  test("leaves other images to Astryx and code alone", () => {
    expect(srcs(render("![chart](https://example.com/a.png)"))).toEqual(["https://example.com/a.png"]);
    const code = render("```\n![](" + png + ")\n```\n\n`![](" + png + ")`");
    expect(srcs(code)).toEqual([]);
    expect(code).toContain(png);
  });

  test("still drops SVG and HTML, and links to data: stay as Astryx has them", () => {
    for (const url of [svg, html]) {
      const out = render(`![x](${url})`);
      expect(srcs(out)).toEqual([]);
      expect(out).not.toContain(`="${url}"`); // HTML comes back as the text it was written as
    }
    expect(render(`[x](${html})`)).not.toContain(`href="${html}"`);
    expect(srcs(render(`[x](${png})`))).toEqual([]);
  });
});
