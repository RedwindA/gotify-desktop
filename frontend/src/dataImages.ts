import { createElement } from "react";
import { createMarkdownPlugin, type MarkdownExtensionNode, type MarkdownTransform } from "@astryxdesign/core/Markdown/plugins";

// Astryx renders no data: image, whatever its type, so a Gotify message with a
// screenshot inlined in base64 (as its web UI shows it) would lose it. This
// plugin shows raster ones itself; SVG, which can carry script, stays out.
// Only the header is checked: a broken payload just fails to load.
const rasterDataImage = /^data:image\/(?:png|jpe?g|gif|webp|avif|bmp);base64,/i;
const anyDataImage = /data:image\//i;

export const isRasterDataImage = (url: string) => rasterDataImage.test(url);

type DataImage = MarkdownExtensionNode<"data-images", "image", { src: string; alt: string }, "inline" | "block">;

// The walk is structural rather than per node type, so containers Astryx adds
// later are covered too. Images whose parent holds blocks are blocks themselves.
type Node = { readonly type: string; readonly url?: string; readonly alt?: string; readonly children?: readonly Node[] };
const blockParents = new Set(["root", "blockquote", "listItem"]);

/** The nodes with their raster data: images replaced, or the same array when there are none. */
function promote(nodes: readonly Node[], display: DataImage["display"]): readonly Node[] {
  let out: Node[] | undefined;
  for (const [i, node] of nodes.entries()) {
    let next = node;
    if (node.type === "image" && node.url && isRasterDataImage(node.url)) {
      const image: DataImage = { type: "extension", plugin: "data-images", name: "image", display, data: { src: node.url, alt: node.alt ?? "" } };
      next = image as Node;
    } else if (node.children && node.type !== "extension") {
      const children = promote(node.children, blockParents.has(node.type) ? "block" : "inline");
      if (children !== node.children) next = { ...node, children };
    }
    if (next !== node) out ??= nodes.slice(0, i);
    out?.push(next);
  }
  return out ?? nodes;
}

const transform: MarkdownTransform<DataImage> = (document, { source }) => {
  if (!anyDataImage.test(source)) return document;
  const children = promote(document.children as readonly Node[], "block");
  return children === document.children ? document : ({ ...document, children } as typeof document);
};

/** Shows the raster data: images of a Markdown body, which Astryx alone would drop. */
export const markdownDataImages = createMarkdownPlugin({
  name: "data-images",
  apiVersion: 1,
  transform,
  renderers: {
    image: {
      render: ({ node }) => createElement("img", { src: node.data.src, alt: node.data.alt }),
      toText: (node) => node.data.alt,
    },
  },
});
