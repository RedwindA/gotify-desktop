import { createElement } from "react";
import type {
  MarkdownAstBlockContent,
  MarkdownAstExtensionNode,
  MarkdownAstListItem,
  MarkdownAstPhrasingContent,
  MarkdownAstTableCell,
  MarkdownAstTableRow,
} from "@astryxdesign/core/Markdown/parser";
import { createMarkdownPlugin, type MarkdownExtensionNode, type MarkdownTransform } from "@astryxdesign/core/Markdown/plugins";

/** Raster types a Markdown image may show as a data: URI; SVG can run script. */
const rasterTypes = new Set(["image/png", "image/jpeg", "image/jpg", "image/gif", "image/webp"]);

/**
 * Whether src is a data: URI of a raster image in base64. javascript:,
 * data:text/html and data:image/svg+xml are not.
 */
export function isSafeDataImageSrc(src: string): boolean {
  return dataImagePayload(src) != null;
}

/** The base64 payload of a safe raster data: image URI, or null. */
export function dataImagePayload(src: string): string | null {
  const s = src.replace(/[\x00-\x1f\x7f]/g, "").trim();
  const mark = ";base64,";
  const idx = s.toLowerCase().indexOf(mark);
  // The media type and its parameters live in a short header; a long match
  // would be in the payload, which is not a media type.
  if (idx < 0 || idx > 80) return null;
  const header = s.slice(0, idx).toLowerCase().replaceAll(" ", "");
  if (!header.startsWith("data:image/")) return null;
  let type = header.slice("data:".length);
  const semi = type.indexOf(";");
  if (semi >= 0) {
    const params = type.slice(semi + 1);
    type = type.slice(0, semi);
    if (params !== "" && !params.split(";").every((p) => p.startsWith("charset="))) return null;
  }
  if (!rasterTypes.has(type)) return null;
  const payload = s.slice(idx + mark.length);
  if (payload === "" || payload.length % 4 !== 0 || !/^[A-Za-z0-9+/]+={0,2}$/.test(payload)) return null;
  return payload;
}

type DataImageNode = MarkdownExtensionNode<"data-images", "image", { src: string; alt: string }, "inline" | "block">;
type Extension = MarkdownAstExtensionNode;
type Phrasing = MarkdownAstPhrasingContent<Extension>;
type Block = MarkdownAstBlockContent<Extension>;

function dataImage(node: { url: string; alt: string }, display: "inline"): (DataImageNode & { display: "inline" }) | null;
function dataImage(node: { url: string; alt: string }, display: "block"): (DataImageNode & { display: "block" }) | null;
function dataImage(node: { url: string; alt: string }, display: DataImageNode["display"]): DataImageNode | null {
  if (!isSafeDataImageSrc(node.url)) return null;
  return { type: "extension", plugin: "data-images", name: "image", display, data: { src: node.url, alt: node.alt } };
}

function transformPhrasing(children: ReadonlyArray<Phrasing>): ReadonlyArray<Phrasing> {
  let next: Phrasing[] | undefined;
  for (let i = 0; i < children.length; i++) {
    const node = children[i];
    if (!node) continue;
    if (node.type === "image") {
      const img = dataImage(node, "inline");
      if (img) {
        next ??= children.slice(0, i);
        next.push(img);
        continue;
      }
    }
    if (node.type === "strong" || node.type === "emphasis" || node.type === "delete" || node.type === "link") {
      const nested = transformPhrasing(node.children);
      if (nested !== node.children) {
        next ??= children.slice(0, i);
        next.push({ ...node, children: nested });
        continue;
      }
    }
    next?.push(node);
  }
  return next ?? children;
}

function transformTableCell(cell: MarkdownAstTableCell<Extension>) {
  const children = transformPhrasing(cell.children);
  return children === cell.children ? cell : { ...cell, children };
}

function transformTableRow(row: MarkdownAstTableRow<Extension>) {
  const children = row.children.map(transformTableCell);
  return children.every((child, i) => child === row.children[i]) ? row : { ...row, children };
}

function transformListItem(item: MarkdownAstListItem<Extension>) {
  const children = transformBlocks(item.children);
  return children === item.children ? item : { ...item, children };
}

function transformBlocks(children: ReadonlyArray<Block>): ReadonlyArray<Block> {
  let next: Block[] | undefined;
  for (let i = 0; i < children.length; i++) {
    const node = children[i];
    if (!node) continue;
    let transformed: Block = node;
    switch (node.type) {
      case "image": {
        transformed = dataImage(node, "block") ?? node;
        break;
      }
      case "heading":
      case "paragraph": {
        const nested = transformPhrasing(node.children);
        if (nested !== node.children) transformed = { ...node, children: nested };
        break;
      }
      case "blockquote": {
        const nested = transformBlocks(node.children);
        if (nested !== node.children) transformed = { ...node, children: nested };
        break;
      }
      case "list": {
        const nested = node.children.map(transformListItem);
        if (nested.some((child, j) => child !== node.children[j])) transformed = { ...node, children: nested };
        break;
      }
      case "table": {
        const nested = node.children.map(transformTableRow);
        if (nested.some((child, j) => child !== node.children[j])) transformed = { ...node, children: nested };
        break;
      }
      case "code":
      case "math":
      case "thematicBreak":
      case "extension":
        break;
      default:
        node satisfies never;
    }
    if (transformed !== node) next ??= children.slice(0, i);
    next?.push(transformed);
  }
  return next ?? children;
}

const transformDataImages: MarkdownTransform<DataImageNode> = (document) => {
  const children = transformBlocks(document.children);
  return children === document.children ? document : { ...document, children };
};

/**
 * Renders Markdown images whose source is a raster data: URI. Astryx's
 * renderer rejects every data: URL, so a safe raster image would otherwise
 * disappear and leave only its alt text.
 */
export const markdownDataImages = createMarkdownPlugin({
  name: "data-images",
  apiVersion: 1,
  transform: transformDataImages,
  renderers: {
    image: {
      render: ({ node }) => createElement("img", { src: node.data.src, alt: node.data.alt }),
      toText: (node) => node.data.alt,
    },
  },
});
