import { describe, expect, test } from "bun:test";
import { parseMarkdownAst } from "@astryxdesign/core/Markdown/parser";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { Markdown } from "@astryxdesign/core/Markdown";
import { InternationalizationProvider } from "@astryxdesign/core/i18n";
import { Theme } from "@astryxdesign/core/theme";
import { neutralTheme } from "@astryxdesign/theme-neutral/built";
import { isSafeDataImageSrc, markdownDataImages } from "../src/dataImages";
import { markdownLinks } from "../src/links";

// 1×1 PNG, the form the Windows client issue uses (JPEG in that report).
const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";
const pngSrc = `data:image/png;base64,${png}`;
const jpegSrc = `data:image/jpeg;base64,${png}`;

const plugins = [...markdownLinks, markdownDataImages];

function images(tree: unknown): { type: string; src?: string; url?: string; plugin?: string }[] {
  const out: { type: string; src?: string; url?: string; plugin?: string }[] = [];
  const walk = (node: unknown) => {
    if (node == null || typeof node !== "object") return;
    const n = node as { type?: string; url?: string; plugin?: string; name?: string; data?: { src?: string }; children?: unknown[] };
    if (n.type === "image") out.push({ type: "image", url: n.url });
    if (n.type === "extension" && n.plugin === "data-images" && n.name === "image") out.push({ type: "data-image", src: n.data?.src });
    if (n.type === "link") out.push({ type: "link", url: n.url });
    if (Array.isArray(n.children)) for (const c of n.children) walk(c);
  };
  walk(tree);
  return out;
}

describe("isSafeDataImageSrc", () => {
  test("allows raster data: images in base64", () => {
    expect(isSafeDataImageSrc(pngSrc)).toBe(true);
    expect(isSafeDataImageSrc(jpegSrc)).toBe(true);
    expect(isSafeDataImageSrc(`data:image/jpg;base64,${png}`)).toBe(true);
    expect(isSafeDataImageSrc(`data:image/gif;base64,${png}`)).toBe(true);
    expect(isSafeDataImageSrc(`data:image/webp;base64,${png}`)).toBe(true);
    expect(isSafeDataImageSrc(`DATA:IMAGE/PNG;BASE64,${png}`)).toBe(true);
    expect(isSafeDataImageSrc(`data: image/png;charset=utf-8;base64,${png}`)).toBe(true);
  });
  test("rejects html, svg, javascript and other data: URIs", () => {
    expect(isSafeDataImageSrc(`data:image/svg+xml;base64,${png}`)).toBe(false);
    expect(isSafeDataImageSrc("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg'></svg>")).toBe(false);
    expect(isSafeDataImageSrc("data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==")).toBe(false);
    expect(isSafeDataImageSrc("javascript:alert(1)")).toBe(false);
    expect(isSafeDataImageSrc("https://example.com/a.png")).toBe(false);
    expect(isSafeDataImageSrc(`data:image/png,${png}`)).toBe(false);
    expect(isSafeDataImageSrc("data:image/png;base64,")).toBe(false);
    expect(isSafeDataImageSrc(`data:image/png;base64,${png}!`)).toBe(false);
  });
});

describe("markdownDataImages", () => {
  const ast = (text: string) => parseMarkdownAst(text, { plugins });

  test("promotes a raster data: image, including the issue's empty-alt form", () => {
    const src = "邮件通知 识别文字: 国家任务达成奖励\n\n![](" + jpegSrc + ")";
    expect(images(ast(src))).toEqual([{ type: "data-image", src: jpegSrc }]);
  });

  test("leaves http(s) images to the default renderer", () => {
    expect(images(ast("![chart](https://example.com/a.png)"))).toEqual([{ type: "image", url: "https://example.com/a.png" }]);
  });

  test("does not promote svg or html data: URIs", () => {
    const svg = "data:image/svg+xml;base64,PHN2Zy8+";
    const html = "data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==";
    expect(images(ast(`![](${svg})`))).toEqual([{ type: "image", url: svg }]);
    // data:text/html is rejected by the parser, so it is not an image node.
    const htmlAst = JSON.stringify(ast(`![](${html})`));
    expect(htmlAst).not.toContain("data-images");
    expect(htmlAst).not.toContain('"type":"image"');
  });

  test("does not widen links", () => {
    const html = "data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==";
    expect(images(ast(`[pic](${pngSrc})`))).toEqual([{ type: "link", url: pngSrc }]);
    expect(JSON.stringify(ast("[x](javascript:alert(1))"))).not.toContain('"type":"link"');
    expect(JSON.stringify(ast(`[x](${html})`))).not.toContain('"type":"link"');
  });

  test("leaves a data: image written in a code block as text", () => {
    const out = JSON.stringify(ast("```\n![](" + pngSrc + ")\n```"));
    expect(out).toContain(pngSrc);
    expect(out).not.toContain("data-images");
  });
});

describe("Markdown rendering", () => {
  function render(text: string, withPlugin = true): string {
    return renderToStaticMarkup(
      createElement(
        Theme,
        { theme: neutralTheme, mode: "light" },
        createElement(
          InternationalizationProvider,
          { locale: "en", messages: {} },
          createElement(Markdown, { density: "compact", contentWidth: "100%", plugins: withPlugin ? plugins : markdownLinks }, text),
        ),
      ),
    );
  }

  test("keeps a raster data: image src and still strips svg and html", () => {
    const body = `告警文字\n\n![](${jpegSrc})`;
    const html = render(body);
    expect(html).toContain(`src="${jpegSrc}"`);
    expect(html).toContain("<img");
    expect(render(body, false)).not.toContain("<img");

    const svg = "data:image/svg+xml;base64,PHN2Zy8+";
    const svgHtml = render(`![](${svg})`);
    expect(svgHtml).not.toContain("<img");
    expect(svgHtml).not.toContain(`src="${svg}"`);

    const dataHtml = "data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==";
    const blocked = render(`![](${dataHtml})`);
    expect(blocked).not.toContain("<img");
    expect(blocked).not.toContain(`src="${dataHtml}"`);
    expect(blocked).not.toContain(`href="${dataHtml}"`);
  });
});
