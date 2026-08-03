import type { SemanticIconName } from "@vastplan/ui-contract";
import { normalizeAntIcon } from "../normalize.js";
import type { IconGlyphDefinition } from "../types.js";
import icon0 from "@ant-design/icons-svg/es/asn/PlusOutlined.js";
import icon1 from "@ant-design/icons-svg/es/asn/DeleteOutlined.js";
import icon2 from "@ant-design/icons-svg/es/asn/EditOutlined.js";
import icon3 from "@ant-design/icons-svg/es/asn/SearchOutlined.js";
import icon4 from "@ant-design/icons-svg/es/asn/SettingOutlined.js";
import icon5 from "@ant-design/icons-svg/es/asn/CheckCircleOutlined.js";
import icon6 from "@ant-design/icons-svg/es/asn/WarningOutlined.js";
import icon7 from "@ant-design/icons-svg/es/asn/CloseCircleOutlined.js";
import icon8 from "@ant-design/icons-svg/es/asn/InfoCircleOutlined.js";
import icon9 from "@ant-design/icons-svg/es/asn/CloseOutlined.js";
import icon10 from "@ant-design/icons-svg/es/asn/MenuOutlined.js";
import icon11 from "@ant-design/icons-svg/es/asn/ImportOutlined.js";
import icon12 from "@ant-design/icons-svg/es/asn/ExportOutlined.js";
import icon13 from "@ant-design/icons-svg/es/asn/CloudUploadOutlined.js";
import icon14 from "@ant-design/icons-svg/es/asn/ReloadOutlined.js";
import icon15 from "@ant-design/icons-svg/es/asn/ColumnHeightOutlined.js";
import icon16 from "@ant-design/icons-svg/es/asn/EyeOutlined.js";
import icon17 from "@ant-design/icons-svg/es/asn/EyeInvisibleOutlined.js";
import icon18 from "@ant-design/icons-svg/es/asn/HolderOutlined.js";
import icon19 from "@ant-design/icons-svg/es/asn/CopyOutlined.js";
import icon20 from "@ant-design/icons-svg/es/asn/DownloadOutlined.js";
import icon21 from "@ant-design/icons-svg/es/asn/UploadOutlined.js";
import icon22 from "@ant-design/icons-svg/es/asn/MoreOutlined.js";
import icon23 from "@ant-design/icons-svg/es/asn/QuestionCircleOutlined.js";
import icon24 from "@ant-design/icons-svg/es/asn/LogoutOutlined.js";
import icon25 from "@ant-design/icons-svg/es/asn/UserOutlined.js";
import icon26 from "@ant-design/icons-svg/es/asn/SlidersOutlined.js";
import icon27 from "@ant-design/icons-svg/es/asn/SafetyCertificateOutlined.js";
import icon28 from "@ant-design/icons-svg/es/asn/ShopOutlined.js";
import icon29 from "@ant-design/icons-svg/es/asn/InboxOutlined.js";
import icon30 from "@ant-design/icons-svg/es/asn/AppstoreOutlined.js";
import icon31 from "@ant-design/icons-svg/es/asn/DeploymentUnitOutlined.js";
import icon32 from "@ant-design/icons-svg/es/asn/LayoutOutlined.js";
import icon33 from "@ant-design/icons-svg/es/asn/DatabaseOutlined.js";
import icon34 from "@ant-design/icons-svg/es/asn/ClusterOutlined.js";
import icon35 from "@ant-design/icons-svg/es/asn/ApiOutlined.js";
import icon36 from "@ant-design/icons-svg/es/asn/SafetyOutlined.js";
import icon37 from "@ant-design/icons-svg/es/asn/KeyOutlined.js";
import icon38 from "@ant-design/icons-svg/es/asn/TableOutlined.js";
import icon39 from "@ant-design/icons-svg/es/asn/ExperimentOutlined.js";

const glyphs: Readonly<Record<SemanticIconName, IconGlyphDefinition>> = Object.freeze({
  "add": normalizeAntIcon(icon0),
  "remove": normalizeAntIcon(icon1),
  "edit": normalizeAntIcon(icon2),
  "search": normalizeAntIcon(icon3),
  "settings": normalizeAntIcon(icon4),
  "success": normalizeAntIcon(icon5),
  "warning": normalizeAntIcon(icon6),
  "error": normalizeAntIcon(icon7),
  "info": normalizeAntIcon(icon8),
  "close": normalizeAntIcon(icon9),
  "menu": normalizeAntIcon(icon10),
  "import": normalizeAntIcon(icon11),
  "export": normalizeAntIcon(icon12),
  "publish": normalizeAntIcon(icon13),
  "refresh": normalizeAntIcon(icon14),
  "columns": normalizeAntIcon(icon15),
  "visibility": normalizeAntIcon(icon16),
  "visibilityOff": normalizeAntIcon(icon17),
  "drag": normalizeAntIcon(icon18),
  "copy": normalizeAntIcon(icon19),
  "download": normalizeAntIcon(icon20),
  "upload": normalizeAntIcon(icon21),
  "more": normalizeAntIcon(icon22),
  "help": normalizeAntIcon(icon23),
  "logout": normalizeAntIcon(icon24),
  "user": normalizeAntIcon(icon25),
  "sliders": normalizeAntIcon(icon26),
  "authentication": normalizeAntIcon(icon27),
  "marketplace": normalizeAntIcon(icon28),
  "repository": normalizeAntIcon(icon29),
  "resources": normalizeAntIcon(icon30),
  "plugins": normalizeAntIcon(icon31),
  "portal": normalizeAntIcon(icon32),
  "database": normalizeAntIcon(icon33),
  "deployment": normalizeAntIcon(icon34),
  "api": normalizeAntIcon(icon35),
  "security": normalizeAntIcon(icon36),
  "credential": normalizeAntIcon(icon37),
  "workbench": normalizeAntIcon(icon38),
  "extension": normalizeAntIcon(icon39),
});

export function semanticIconGlyph(name: SemanticIconName): IconGlyphDefinition { return glyphs[name]; }
