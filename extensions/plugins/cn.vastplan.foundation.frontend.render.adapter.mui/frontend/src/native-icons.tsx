import type { ComponentType } from "react";
import type { SvgIconProps } from "@mui/material/SvgIcon";
import AddOutlined from "@mui/icons-material/AddOutlined";
import CheckCircleOutline from "@mui/icons-material/CheckCircleOutline";
import Close from "@mui/icons-material/Close";
import ContentCopyOutlined from "@mui/icons-material/ContentCopyOutlined";
import DeleteOutline from "@mui/icons-material/DeleteOutline";
import DownloadOutlined from "@mui/icons-material/DownloadOutlined";
import EditOutlined from "@mui/icons-material/EditOutlined";
import ErrorOutline from "@mui/icons-material/ErrorOutline";
import FileDownloadOutlined from "@mui/icons-material/FileDownloadOutlined";
import FileUploadOutlined from "@mui/icons-material/FileUploadOutlined";
import InfoOutlined from "@mui/icons-material/InfoOutlined";
import Menu from "@mui/icons-material/Menu";
import MoreHoriz from "@mui/icons-material/MoreHoriz";
import PublishOutlined from "@mui/icons-material/PublishOutlined";
import RefreshOutlined from "@mui/icons-material/RefreshOutlined";
import SearchOutlined from "@mui/icons-material/SearchOutlined";
import SettingsOutlined from "@mui/icons-material/SettingsOutlined";
import UploadOutlined from "@mui/icons-material/UploadOutlined";
import ViewColumnOutlined from "@mui/icons-material/ViewColumnOutlined";
import WarningAmberOutlined from "@mui/icons-material/WarningAmberOutlined";
import { VastPlanIcon } from "@vastplan/ui-primitives";
import type { SemanticIconName, VastPlanIconProps } from "@vastplan/ui-primitives";

const nativeIcons: Partial<Record<SemanticIconName, ComponentType<SvgIconProps>>> = {
  add: AddOutlined,
  remove: DeleteOutline,
  edit: EditOutlined,
  search: SearchOutlined,
  settings: SettingsOutlined,
  success: CheckCircleOutline,
  warning: WarningAmberOutlined,
  error: ErrorOutline,
  info: InfoOutlined,
  close: Close,
  menu: Menu,
  import: FileDownloadOutlined,
  export: FileUploadOutlined,
  publish: PublishOutlined,
  refresh: RefreshOutlined,
  columns: ViewColumnOutlined,
  copy: ContentCopyOutlined,
  download: DownloadOutlined,
  upload: UploadOutlined,
  more: MoreHoriz,
};

const pixels = { sm: 16, md: 20, lg: 24 } as const;

export function MuiNativeIcon({ name, label, size = "md", className, style }: VastPlanIconProps) {
  const Icon = nativeIcons[name];
  if (Icon === undefined) return <VastPlanIcon name={name} label={label} size={size} className={className} style={style} />;
  return <Icon
    data-vastplan-icon={name}
    data-vastplan-icon-source="renderer-native"
    className={className}
    sx={{ fontSize: pixels[size], ...style }}
    titleAccess={label}
    aria-label={label}
    aria-hidden={label === undefined ? true : undefined}
  />;
}
