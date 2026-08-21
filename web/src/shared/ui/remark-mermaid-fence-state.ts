type MarkdownNode = {
  type?: string;
  lang?: string | null;
  children?: MarkdownNode[];
  position?: {
    start?: { offset?: number };
    end?: { offset?: number };
  };
  data?: {
    hProperties?: Record<string, unknown>;
  };
};

type MarkdownFile = { value: unknown };

export function remarkMermaidFenceState() {
  return (tree: MarkdownNode, file: MarkdownFile) => {
    const markdown = String(file.value ?? '');
    visitMarkdownNodes(tree, (node) => {
      if (node.type !== 'code' || node.lang?.toLowerCase() !== 'mermaid') return;

      const start = node.position?.start?.offset;
      const end = node.position?.end?.offset;
      if (start === undefined || end === undefined) return;

      node.data ??= {};
      node.data.hProperties ??= {};
      node.data.hProperties['data-mermaid-closed'] = isClosedFence(markdown.slice(start, end));
    });
  };
}

function visitMarkdownNodes(node: MarkdownNode, visitor: (node: MarkdownNode) => void) {
  visitor(node);
  node.children?.forEach((child) => visitMarkdownNodes(child, visitor));
}

function isClosedFence(source: string) {
  const lines = source.split('\n');
  const openingMarker = lines[0]?.match(/^ {0,3}(`{3,}|~{3,})/)?.[1];
  const closingMarker = lines.at(-1)?.match(/^ {0,3}(`+|~+)[\t ]*$/)?.[1];
  return (
    openingMarker !== undefined &&
    closingMarker !== undefined &&
    openingMarker[0] === closingMarker[0] &&
    closingMarker.length >= openingMarker.length
  );
}
