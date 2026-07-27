import type { ComponentType, KeyboardEvent, ReactNode } from "react";
import type { SemanticIconName } from "./icon.js";
import type { FormRendererProps } from "./form-renderer.js";

export interface MenuItem {
  id: string;
  label: ReactNode;
  icon?: ReactNode;
  href?: string;
  disabled?: boolean;
  children?: MenuItem[];
}

export interface PageProps {
  title?: string;
  children: ReactNode;
  actions?: ReactNode;
}

export type SpaceSize = "xs" | "sm" | "md" | "lg";
export type ResponsiveColumns = number | { xs?: number; sm?: number; md?: number; lg?: number; xl?: number };

export interface PortalShellProps {
  header?: ReactNode;
  navigation?: ReactNode;
  inspector?: ReactNode;
  statusBar?: ReactNode;
  children: ReactNode;
}

export interface StackProps {
  direction?: "row" | "column";
  gap?: SpaceSize;
  align?: "start" | "center" | "end" | "stretch";
  justify?: "start" | "center" | "end" | "between";
  wrap?: boolean;
  children: ReactNode;
}

export interface GridProps {
  columns?: ResponsiveColumns;
  gap?: SpaceSize;
  children: ReactNode;
}

export interface GridItemProps { span?: ResponsiveColumns; children: ReactNode; }

export interface PanelProps {
  title?: string;
  children: ReactNode;
}

export interface ButtonProps {
  children: ReactNode;
  onClick?(): void;
  disabled?: boolean;
  loading?: boolean;
  kind?: "primary" | "secondary" | "danger" | "text";
}

export interface IconButtonProps {
  icon: SemanticIconName;
  label: string;
  onClick?(): void;
  disabled?: boolean;
  loading?: boolean;
  tone?: "normal" | "primary" | "danger";
}

export interface SelectOption {
  value: string;
  label: ReactNode;
  disabled?: boolean;
}

export interface SelectProps {
  value?: string;
  options: readonly SelectOption[];
  placeholder?: string;
  ariaLabel: string;
  disabled?: boolean;
  onChange(value: string | undefined): void;
}

export interface BreadcrumbItem { id: string; label: string; href?: string; onSelect?(): void; }
export interface TabItem { id: string; label: ReactNode; content: ReactNode; disabled?: boolean; }

export interface DialogProps {
  open: boolean;
  title: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
  width?: "sm" | "md" | "lg";
  onClose(): void;
}

export interface DrawerProps extends DialogProps { placement?: "top" | "right" | "bottom" | "left"; }

export type PopoverPlacement = "bottom-start" | "bottom" | "bottom-end" | "top-start" | "top" | "top-end";
export type PopoverCloseReason = "trigger" | "escape" | "outside" | "selection";
export interface PopoverTriggerProps {
  ref(node: HTMLElement | null): void;
  "aria-expanded": boolean;
  "aria-controls": string;
  onClick(): void;
  onKeyDown(event: KeyboardEvent<HTMLElement>): void;
}
export interface PopoverProps {
  open: boolean;
  trigger(props: PopoverTriggerProps): ReactNode;
  children: ReactNode;
  placement?: PopoverPlacement;
  initialFocus?: "current" | "first" | "none";
  ariaLabel?: string;
  onOpenChange(open: boolean, reason: PopoverCloseReason): void;
}

export interface CommandItem {
  id: string;
  title: string;
  description?: string;
  keywords?: string[];
  disabled?: boolean;
  run(): void;
}

export interface TableColumn {
  key: string;
  title: ReactNode;
  width?: number;
  /** A governed alignment hint used by structural columns such as row actions. */
  align?: "start" | "center" | "end";
  /** Structural columns may stay visible while the collection scrolls horizontally. */
  fixed?: "right";
  render?(value: unknown, row: Readonly<Record<string, unknown>>, index: number): ReactNode;
}

export interface TableProps {
  columns: TableColumn[];
  rows: ReadonlyArray<Readonly<Record<string, unknown>>>;
  rowKey?: string | ((row: Readonly<Record<string, unknown>>) => string);
  selection?: "none" | "single" | "multiple";
  selectedRowKeys?: readonly string[];
  onSelectionChange?(keys: readonly string[]): void;
  loading?: boolean;
  empty?: ReactNode;
  density?: "compact" | "standard" | "comfortable";
  /** A governed visual treatment owned by the render adapter, never by a functional plugin. */
  appearance?: "default" | "collection";
}

export interface DataCardProps {
  title: ReactNode;
  subtitle?: ReactNode;
  status?: ReactNode;
  summary?: ReactNode;
  children?: ReactNode;
  actions?: ReactNode;
  selectable?: boolean;
  selected?: boolean;
  selectionLabel?: string;
  density?: "compact" | "standard" | "comfortable";
  onSelectionChange?(selected: boolean): void;
}

export interface SplitViewProps {
  primaryLabel: string;
  secondaryLabel: string;
  primary: ReactNode;
  secondary: ReactNode;
  mode?: "both" | "primary" | "secondary";
  primaryWidth?: "sm" | "md" | "lg";
}

export interface RecordNavigationItem {
  id: string;
  title: ReactNode;
  description?: ReactNode;
  status?: ReactNode;
  disabled?: boolean;
}

export interface RecordNavigationListProps {
  items: readonly RecordNavigationItem[];
  selectedID?: string;
  ariaLabel: string;
  onSelect(id: string): void;
}

export interface RecordTreeItem extends RecordNavigationItem {
  children?: readonly RecordTreeItem[];
}

export interface RecordTreeProps {
  items: readonly RecordTreeItem[];
  selectedID?: string;
  expandedIDs: readonly string[];
  ariaLabel: string;
  onSelect(id: string): void;
  onExpandedChange(ids: readonly string[]): void;
}

export interface FilterBarProps {
  children: ReactNode;
  actions?: ReactNode;
  /** Collection is borderless and must contribute no outer margin or inset. */
  appearance?: "default" | "collection";
}

export interface PaginationProps {
  page: number;
  pageSize: number;
  pageSizeOptions?: readonly number[];
  total: number;
  disabled?: boolean;
  align?: "start" | "center" | "end";
  onChange(page: number, pageSize: number): void;
}

export interface DescriptionItem { id: string; label: ReactNode; value: ReactNode; }
export type StatusTone = "neutral" | "info" | "success" | "warning" | "error";
export interface SemanticThemeTokens {
  color: {
    canvas: string; surface: string; overlaySurface: string; text: string; mutedText: string; border: string;
    primary: string; danger: string; warning: string; success: string; hover: string; selected: string; focusRing: string;
  };
  radius: { sm: number; md: number; lg: number; };
  spacing: Record<SpaceSize, number>;
  shell: { barHeight: number; railWidth: number; navigationWidth: number; navigationCompactWidth: number; };
  overlay: { navigationMinWidth: number; navigationMaxWidth: number; };
  elevation: { overlay: string; };
  motion: { fast: number; normal: number; };
  focus: { width: number; };
  touch: { minimum: number; };
}

export interface PortalUI {
  PortalShell: ComponentType<PortalShellProps>;
  Page: ComponentType<PageProps>;
  Panel: ComponentType<PanelProps>;
  Stack: ComponentType<StackProps>;
  Grid: ComponentType<GridProps>;
  GridItem: ComponentType<GridItemProps>;
  Divider: ComponentType<{ label?: ReactNode }>;
  Button: ComponentType<ButtonProps>;
  IconButton: ComponentType<IconButtonProps>;
  Select: ComponentType<SelectProps>;
  Menu: ComponentType<{ items: MenuItem[]; activeID?: string; onSelect?(id: string): void }>;
  Breadcrumb: ComponentType<{ items: BreadcrumbItem[] }>;
  Tabs: ComponentType<{ items: TabItem[]; activeID?: string; onChange?(id: string): void }>;
  CommandPalette: ComponentType<{ open: boolean; commands: CommandItem[]; query: string; onQueryChange(query: string): void; onClose(): void }>;
  Popover: ComponentType<PopoverProps>;
  Dialog: ComponentType<DialogProps>;
  Drawer: ComponentType<DrawerProps>;
  FormRenderer: ComponentType<FormRendererProps>;
  FilterBar: ComponentType<FilterBarProps>;
  Table: ComponentType<TableProps>;
  DataCard: ComponentType<DataCardProps>;
  SplitView: ComponentType<SplitViewProps>;
  RecordNavigationList: ComponentType<RecordNavigationListProps>;
  RecordTree: ComponentType<RecordTreeProps>;
  Pagination: ComponentType<PaginationProps>;
  Descriptions: ComponentType<{ title?: ReactNode; items: DescriptionItem[]; columns?: ResponsiveColumns }>;
  Status: ComponentType<{ tone?: StatusTone; children: ReactNode }>;
  Icon: ComponentType<import("./icon.js").VastPlanIconProps>;
  theme: { mode: "light" | "dark" | "system"; tokens: SemanticThemeTokens };
  EmptyState: ComponentType<{ title: string; description?: string }>;
  ErrorState: ComponentType<{ title: string; retry?(): void }>;
  Skeleton: ComponentType<{ rows?: number }>;
  Busy: ComponentType<{ label?: string }>;
  notify(message: { title: string; content?: string; kind?: "info" | "success" | "warning" | "error" }): void;
  confirm(message: { title: string; content?: string }): Promise<boolean>;
}

/**
 * A named, framework-neutral presentation template exposed by a render adapter.
 *
 * The descriptor deliberately contains semantic intent only.  Its implementation
 * belongs to the selected adapter: for example Arco maps `dark` to its native
 * CSS theme attribute, while MUI maps it to `createTheme({ palette.mode })`.
 */
