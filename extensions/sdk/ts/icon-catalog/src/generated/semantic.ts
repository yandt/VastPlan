import type { SemanticIconName } from "@vastplan/ui-contract";
import { normalizeAntIcon } from "../normalize.js";
import type { IconGlyphDefinition } from "../types.js";
import icon0 from "@ant-design/icons-svg/es/asn/PlusOutlined.js";
import icon1 from "@ant-design/icons-svg/es/asn/MinusOutlined.js";
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
});

export function semanticIconGlyph(name: SemanticIconName): IconGlyphDefinition { return glyphs[name]; }
