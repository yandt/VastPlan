import PlusOutlined from "@ant-design/icons/PlusOutlined";
import DeleteOutlined from "@ant-design/icons/DeleteOutlined";
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
import QuestionCircleOutlined from "@ant-design/icons/QuestionCircleOutlined";
import EyeOutlined from "@ant-design/icons/EyeOutlined";
import EyeInvisibleOutlined from "@ant-design/icons/EyeInvisibleOutlined";
import HolderOutlined from "@ant-design/icons/HolderOutlined";
import LogoutOutlined from "@ant-design/icons/LogoutOutlined";
import UserOutlined from "@ant-design/icons/UserOutlined";
import SlidersOutlined from "@ant-design/icons/SlidersOutlined";
import SafetyCertificateOutlined from "@ant-design/icons/SafetyCertificateOutlined";
import ShopOutlined from "@ant-design/icons/ShopOutlined";
import InboxOutlined from "@ant-design/icons/InboxOutlined";
import AppstoreOutlined from "@ant-design/icons/AppstoreOutlined";
import DeploymentUnitOutlined from "@ant-design/icons/DeploymentUnitOutlined";
import LayoutOutlined from "@ant-design/icons/LayoutOutlined";
import DatabaseOutlined from "@ant-design/icons/DatabaseOutlined";
import ClusterOutlined from "@ant-design/icons/ClusterOutlined";
import ApiOutlined from "@ant-design/icons/ApiOutlined";
import SafetyOutlined from "@ant-design/icons/SafetyOutlined";
import KeyOutlined from "@ant-design/icons/KeyOutlined";
import TableOutlined from "@ant-design/icons/TableOutlined";
import ExperimentOutlined from "@ant-design/icons/ExperimentOutlined";
import { useComponentSize, type SemanticIconName, type VastPlanIconProps } from "@vastplan/ui-primitives";

const icons: Record<SemanticIconName, typeof PlusOutlined> = {
  add: PlusOutlined, remove: DeleteOutlined, edit: EditOutlined, search: SearchOutlined, settings: SettingOutlined,
  success: CheckCircleOutlined, warning: WarningOutlined, error: CloseCircleOutlined, info: InfoCircleOutlined,
  close: CloseOutlined, menu: MenuOutlined, import: ImportOutlined, export: ExportOutlined, publish: CloudUploadOutlined,
  refresh: ReloadOutlined, columns: ColumnHeightOutlined, visibility: EyeOutlined, visibilityOff: EyeInvisibleOutlined, drag: HolderOutlined,
  copy: CopyOutlined, download: DownloadOutlined, upload: UploadOutlined, more: MoreOutlined, help: QuestionCircleOutlined, logout: LogoutOutlined,
  user: UserOutlined, sliders: SlidersOutlined, authentication: SafetyCertificateOutlined, marketplace: ShopOutlined, repository: InboxOutlined,
  resources: AppstoreOutlined, plugins: DeploymentUnitOutlined, portal: LayoutOutlined, database: DatabaseOutlined, deployment: ClusterOutlined,
  api: ApiOutlined, security: SafetyOutlined, credential: KeyOutlined, workbench: TableOutlined, extension: ExperimentOutlined,
};

const pixels = { xs: 14, sm: 16, md: 20, lg: 24 } as const;

export function AntdNativeIcon({ name, label, size: requestedSize, style }: VastPlanIconProps) {
  const Icon = icons[name];
  const size = useComponentSize(requestedSize);
  return <span data-vastplan-icon={name} data-vastplan-icon-source="renderer-native"><Icon
    role={label === undefined ? undefined : "img"}
    aria-label={label}
    aria-hidden={label === undefined ? true : undefined}
    style={{ fontSize: pixels[size], ...style }}
  /></span>;
}
