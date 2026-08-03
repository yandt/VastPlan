import { VastPlanIcon } from "@vastplan/ui-primitives";
import type { PortalUI } from "@vastplan/ui-primitives";
import { Button, Busy, Dialog, Drawer, EmptyState, ErrorState, IconButton, Skeleton, Tooltip } from "./feedback";
import { DataCard, Descriptions, Pagination, Select, Status, Table } from "./data";
import { BodySections, Divider, FilterBar, Grid, GridItem, Page, Panel, PortalShell, SplitView, Stack } from "./layout";
import { Breadcrumb, CommandPalette, Menu, Popover, RecordNavigationList, RecordTree, Tabs } from "./navigation";
import { FormRenderer } from "./form-renderer";
import { semanticTokens } from "./theme";

export type AntdComponents = Omit<PortalUI, "notify" | "confirm">;

export const antdPortalUIComponents: AntdComponents = {
  PortalShell,
  Page,
  Panel,
  BodySections,
  Stack,
  Grid,
  GridItem,
  Divider,
  Button,
  IconButton,
  Select,
  Menu,
  Breadcrumb,
  Tabs,
  CommandPalette,
  Tooltip,
  Popover,
  Dialog,
  Drawer,
  FormRenderer,
  FilterBar,
  Table,
  DataCard,
  SplitView,
  RecordNavigationList,
  RecordTree,
  Pagination,
  Descriptions,
  Status,
  Icon: VastPlanIcon,
  theme: { mode: "system", tokens: semanticTokens },
  EmptyState,
  ErrorState,
  Skeleton,
  Busy,
};
