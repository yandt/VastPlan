import type { ComponentType } from "react";
import { VastPlanIcon } from "@vastplan/ui-primitives";
import type { SemanticIconName, VastPlanIconProps } from "@vastplan/ui-primitives";
import {
  IconCheckCircle,
  IconClose,
  IconCloseCircle,
  IconCopy,
  IconDelete,
  IconDownload,
  IconEdit,
  IconExclamationCircle,
  IconExport,
  IconImport,
  IconInfoCircle,
  IconLayout,
  IconMenu,
  IconMore,
  IconPlus,
  IconRefresh,
  IconSearch,
  IconSettings,
  IconUpload,
} from "./arco-components";

const nativeIcons: Partial<Record<SemanticIconName, ComponentType<Record<string, unknown>>>> = {
  add: IconPlus,
  remove: IconDelete,
  edit: IconEdit,
  search: IconSearch,
  settings: IconSettings,
  success: IconCheckCircle,
  warning: IconExclamationCircle,
  error: IconCloseCircle,
  info: IconInfoCircle,
  close: IconClose,
  menu: IconMenu,
  import: IconImport,
  export: IconExport,
  publish: IconUpload,
  refresh: IconRefresh,
  columns: IconLayout,
  copy: IconCopy,
  download: IconDownload,
  upload: IconUpload,
  more: IconMore,
};

const pixels = { sm: 16, md: 20, lg: 24 } as const;

export function ArcoNativeIcon({ name, label, size = "md", className, style }: VastPlanIconProps) {
  const Icon = nativeIcons[name];
  if (Icon === undefined) return <VastPlanIcon name={name} label={label} size={size} className={className} style={style} />;
  return <Icon
    data-vastplan-icon={name}
    data-vastplan-icon-source="renderer-native"
    className={className}
    style={{ fontSize: pixels[size], ...style }}
    role={label === undefined ? undefined : "img"}
    aria-label={label}
    aria-hidden={label === undefined ? true : undefined}
  />;
}
