import { createMarkdownPlugin, createMarkdownTextTransform } from "@astryxdesign/core/Markdown/plugins";

// Bare links in message text, after GFM's autolink literals: http(s)://, www. and e-mail addresses,
// and <https://…>, <mailto:…> or <address>, which markdown would otherwise show with their brackets.
// A link ends at whitespace, <, > or ", or at CJK and full-width punctuation ("见 https://a.com，谢谢").
// Sentence punctuation at its end stays outside, and a ")" only when it closes a "(" within the link.
const stop = String.raw`\s<>"()\u3000-\u303f\uff00-\uffef`;
const group = String.raw`\([^${stop}]*\)`;
const body = String.raw`(?:[^${stop}]|${group})*(?:[^${stop}?!.,:;*_~']|${group})`;
const address = String.raw`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)+`;
const linkPattern = new RegExp(String.raw`<((?:https?:\/\/|mailto:)[^\s<>]+|${address})>|(?:https?:\/\/|www\.)${body}|${address}`, "g");

/** The end of the link the pattern matched, or false when it is the tail of a longer word. */
function linkEnd(text: string, m: RegExpExecArray): number | false {
  if (m[1]) return m.index + m[0].length;
  const prev = text[m.index - 1] ?? "";
  const word = /^(https?:\/\/|www\.)/.test(m[0]) ? /[A-Za-z0-9/=]/ : /[A-Za-z0-9._%+@-]/;
  return word.test(prev) ? false : m.index + m[0].length;
}

/** Where a link goes: www. gets http://, and an address mailto:. */
export function linkHref(link: string): string {
  if (/^(https?:\/\/|mailto:)/.test(link)) return link;
  return link.startsWith("www.") ? "http://" + link : "mailto:" + link;
}

export interface FoundLink {
  start: number;
  end: number;
  text: string;
  href: string;
}

/** The bare links in plain text, in order; angle brackets stay outside them. */
export function findLinks(text: string): FoundLink[] {
  const links: FoundLink[] = [];
  const re = new RegExp(linkPattern);
  for (let m; (m = re.exec(text)); ) {
    if (linkEnd(text, m) === false) continue;
    const link = m[1] ?? m[0];
    const start = m[1] ? m.index + 1 : m.index;
    links.push({ start, end: start + link.length, text: link, href: linkHref(link) });
  }
  return links;
}

/** Bare links in markdown prose; code, existing links and images are left alone. */
export const markdownLinks = [
  createMarkdownPlugin({
    name: "bare-links",
    apiVersion: 1,
    transform: createMarkdownTextTransform({
      pattern: linkPattern,
      requiredSubstrings: ["http://", "https://", "www.", "@"],
      getEndIndex: linkEnd,
      replace: (m) => ({ type: "link", url: linkHref(m[1] ?? m[0]), children: [{ type: "text", value: m[1] ?? m[0] }] }),
    }),
  }),
];
