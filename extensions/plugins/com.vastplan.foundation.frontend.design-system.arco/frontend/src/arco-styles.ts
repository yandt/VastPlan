// This is the transitive style closure of the components exported by
// arco-components.ts. Keep the base layer first and dependency styles before
// their consumers. The bundle-size gate catches accidental full-theme imports.
import base from "@arco-design/web-react/es/style/index.css";
import overflowEllipsis from "@arco-design/web-react/es/_class/OverflowEllipsis/style/index.css";
import picker from "@arco-design/web-react/es/_class/picker/style/index.css";
import affix from "@arco-design/web-react/es/Affix/style/index.css";
import alert from "@arco-design/web-react/es/Alert/style/index.css";
import button from "@arco-design/web-react/es/Button/style/index.css";
import checkbox from "@arco-design/web-react/es/Checkbox/style/index.css";
import empty from "@arco-design/web-react/es/Empty/style/index.css";
import input from "@arco-design/web-react/es/Input/style/index.css";
import inputTag from "@arco-design/web-react/es/InputTag/style/index.css";
import link from "@arco-design/web-react/es/Link/style/index.css";
import popover from "@arco-design/web-react/es/Popover/style/index.css";
import radio from "@arco-design/web-react/es/Radio/style/index.css";
import resizeBox from "@arco-design/web-react/es/ResizeBox/style/index.css";
import space from "@arco-design/web-react/es/Space/style/index.css";
import spin from "@arco-design/web-react/es/Spin/style/index.css";
import tooltip from "@arco-design/web-react/es/Tooltip/style/index.css";
import trigger from "@arco-design/web-react/es/Trigger/style/index.css";
import breadcrumb from "@arco-design/web-react/es/Breadcrumb/style/index.css";
import card from "@arco-design/web-react/es/Card/style/index.css";
import descriptions from "@arco-design/web-react/es/Descriptions/style/index.css";
import divider from "@arco-design/web-react/es/Divider/style/index.css";
import dropdown from "@arco-design/web-react/es/Dropdown/style/index.css";
import grid from "@arco-design/web-react/es/Grid/style/index.css";
import inputNumber from "@arco-design/web-react/es/InputNumber/style/index.css";
import layout from "@arco-design/web-react/es/Layout/style/index.css";
import menu from "@arco-design/web-react/es/Menu/style/index.css";
import modal from "@arco-design/web-react/es/Modal/style/index.css";
import notification from "@arco-design/web-react/es/Notification/style/index.css";
import rate from "@arco-design/web-react/es/Rate/style/index.css";
import select from "@arco-design/web-react/es/Select/style/index.css";
import skeleton from "@arco-design/web-react/es/Skeleton/style/index.css";
import switchStyle from "@arco-design/web-react/es/Switch/style/index.css";
import tabs from "@arco-design/web-react/es/Tabs/style/index.css";
import tag from "@arco-design/web-react/es/Tag/style/index.css";
import timePicker from "@arco-design/web-react/es/TimePicker/style/index.css";
import typography from "@arco-design/web-react/es/Typography/style/index.css";
import colorPicker from "@arco-design/web-react/es/ColorPicker/style/index.css";
import datePicker from "@arco-design/web-react/es/DatePicker/style/index.css";
import drawer from "@arco-design/web-react/es/Drawer/style/index.css";
import form from "@arco-design/web-react/es/Form/style/index.css";
import pagination from "@arco-design/web-react/es/Pagination/style/index.css";
import table from "@arco-design/web-react/es/Table/style/index.css";

export const arcoCSS = [
  base,
  overflowEllipsis,
  picker,
  affix,
  alert,
  button,
  checkbox,
  empty,
  input,
  inputTag,
  link,
  popover,
  radio,
  resizeBox,
  space,
  spin,
  tooltip,
  trigger,
  breadcrumb,
  card,
  descriptions,
  divider,
  dropdown,
  grid,
  inputNumber,
  layout,
  menu,
  modal,
  notification,
  rate,
  select,
  skeleton,
  switchStyle,
  tabs,
  tag,
  timePicker,
  typography,
  colorPicker,
  datePicker,
  drawer,
  form,
  pagination,
  table,
].join("\n");
