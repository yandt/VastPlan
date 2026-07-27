import PlusOutlined from "@ant-design/icons/PlusOutlined";
import MinusOutlined from "@ant-design/icons/MinusOutlined";
import EditOutlined from "@ant-design/icons/EditOutlined";
import SearchOutlined from "@ant-design/icons/SearchOutlined";
import SettingOutlined from "@ant-design/icons/SettingOutlined";
import CheckCircleOutlined from "@ant-design/icons/CheckCircleOutlined";
import WarningOutlined from "@ant-design/icons/WarningOutlined";
import CloseCircleOutlined from "@ant-design/icons/CloseCircleOutlined";
import InfoCircleOutlined from "@ant-design/icons/InfoCircleOutlined";
import CloseOutlined from "@ant-design/icons/CloseOutlined";
import MenuOutlined from "@ant-design/icons/MenuOutlined";
import ImportOutlined from "@ant-design/icons/ImportOutlined";
import ExportOutlined from "@ant-design/icons/ExportOutlined";
import CloudUploadOutlined from "@ant-design/icons/CloudUploadOutlined";
import ReloadOutlined from "@ant-design/icons/ReloadOutlined";
import ColumnHeightOutlined from "@ant-design/icons/ColumnHeightOutlined";
import CopyOutlined from "@ant-design/icons/CopyOutlined";
import DownloadOutlined from "@ant-design/icons/DownloadOutlined";
import UploadOutlined from "@ant-design/icons/UploadOutlined";
import MoreOutlined from "@ant-design/icons/MoreOutlined";
import EyeOutlined from "@ant-design/icons/EyeOutlined";
import EyeInvisibleOutlined from "@ant-design/icons/EyeInvisibleOutlined";
import HolderOutlined from "@ant-design/icons/HolderOutlined";
import type { SemanticIconName, VastPlanIconProps } from "@vastplan/ui-primitives";

const icons: Record<SemanticIconName, typeof PlusOutlined> = {
  add: PlusOutlined, remove: MinusOutlined, edit: EditOutlined, search: SearchOutlined, settings: SettingOutlined,
  success: CheckCircleOutlined, warning: WarningOutlined, error: CloseCircleOutlined, info: InfoCircleOutlined,
  close: CloseOutlined, menu: MenuOutlined, import: ImportOutlined, export: ExportOutlined, publish: CloudUploadOutlined,
  refresh: ReloadOutlined, columns: ColumnHeightOutlined, visibility: EyeOutlined, visibilityOff: EyeInvisibleOutlined, drag: HolderOutlined,
  copy: CopyOutlined, download: DownloadOutlined, upload: UploadOutlined, more: MoreOutlined,
};

const pixels = { sm: 16, md: 20, lg: 24 } as const;

export function AntdNativeIcon({ name, label, size = "md", style }: VastPlanIconProps) {
  const Icon = icons[name];
  return <span data-vastplan-icon={name} data-vastplan-icon-source="renderer-native"><Icon
    role={label === undefined ? undefined : "img"}
    aria-label={label}
    aria-hidden={label === undefined ? true : undefined}
    style={{ fontSize: pixels[size], ...style }}
  /></span>;
}
