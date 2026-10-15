import { describe, expect, test } from "bun:test";
import { parseMarkdown } from "@astryxdesign/core/Markdown/parser";
import { findLinks, markdownLinks } from "../src/links";

const links = (text: string) => findLinks(text).map((l) => [l.text, l.href]);

describe("findLinks", () => {
  test("finds http(s), www. and addresses", () => {
    expect(links("see https://example.com/a?b=1&c=2#d and www.example.org or ops@example.com")).toEqual([
      ["https://example.com/a?b=1&c=2#d", "https://example.com/a?b=1&c=2#d"],
      ["www.example.org", "http://www.example.org"],
      ["ops@example.com", "mailto:ops@example.com"],
    ]);
  });
  test("leaves sentence punctuation and unmatched parentheses out", () => {
    expect(links("Go to https://example.com/status.")).toEqual([["https://example.com/status", "https://example.com/status"]]);
    expect(links("(see https://example.com/x)")).toEqual([["https://example.com/x", "https://example.com/x"]]);
    expect(links("https://en.wikipedia.org/wiki/Go_(language), ok")).toEqual([
      ["https://en.wikipedia.org/wiki/Go_(language)", "https://en.wikipedia.org/wiki/Go_(language)"],
    ]);
  });
  test("ends at CJK and full-width punctuation", () => {
    expect(links("详情见https://example.com/x，请查看。邮箱ops@example.com。")).toEqual([
      ["https://example.com/x", "https://example.com/x"],
      ["ops@example.com", "mailto:ops@example.com"],
    ]);
    expect(links("链接：https://example.com/中文页面 完")).toEqual([["https://example.com/中文页面", "https://example.com/中文页面"]]);
  });
  test("skips the tail of a longer word", () => {
    expect(links("xhttps://example.com a=www.example.com")).toEqual([]);
    expect(links("https://user@example.com/x")).toEqual([["https://user@example.com/x", "https://user@example.com/x"]]);
    expect(links("http:// and www. alone")).toEqual([]);
  });
  test("keeps angle brackets outside the link", () => {
    expect(findLinks("<https://example.com/a b> <ops@example.com>")).toEqual([
      { start: 1, end: 22, text: "https://example.com/a", href: "https://example.com/a" },
      { start: 27, end: 42, text: "ops@example.com", href: "mailto:ops@example.com" },
    ]);
  });
});

describe("markdownLinks", () => {
  const md = (text: string) => JSON.stringify(parseMarkdown(text, { plugins: markdownLinks }));
  test("links bare URLs in prose", () => {
    const out = md("**Disk** at https://grafana.example/d/disk_usage?x=1, mail ops@example.com");
    expect(out).toContain('"href":"https://grafana.example/d/disk_usage?x=1"');
    expect(out).toContain('"href":"mailto:ops@example.com"');
  });
  test("leaves existing links and code alone", () => {
    const out = md("[dash](https://a.example/x) `https://b.example` <https://c.example>\n\n```\nhttps://d.example\n```");
    expect(out.match(/"type":"link"/g)?.length).toBe(2);
    expect(out).toContain('{"type":"text","content":" "},{"type":"link","href":"https://c.example"');
    expect(out).not.toContain('"href":"https://b.example"');
    expect(out).not.toContain('"href":"https://d.example"');
  });
});
