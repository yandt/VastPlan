export type IconTone = "primary" | "secondary";

export interface IconPathNode {
  readonly tag: "path";
  readonly d: string;
  readonly tone: IconTone;
  readonly opacity?: number;
  readonly fillRule?: "evenodd" | "nonzero";
}

export interface IconGroupNode {
  readonly tag: "g";
  readonly transform?: string;
  readonly children: readonly IconGlyphNode[];
}

export type IconGlyphNode = IconPathNode | IconGroupNode;

export interface IconGlyphDefinition {
  readonly viewBox: string;
  readonly fillRule?: "evenodd" | "nonzero";
  readonly nodes: readonly IconGlyphNode[];
}

export interface IconCatalogEntry {
  readonly name: string;
  readonly component: string;
  readonly sourceName: string;
  readonly theme: "outlined" | "filled" | "twotone";
}
