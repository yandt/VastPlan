import type { IconCatalogEntry } from "../../types.js";

export const shard03Entries = Object.freeze([
  {"name":"arrow-down-outlined","component":"ArrowDownOutlined","sourceName":"arrow-down","theme":"outlined"},
  {"name":"bug-filled","component":"BugFilled","sourceName":"bug","theme":"filled"},
  {"name":"bulb-outlined","component":"BulbOutlined","sourceName":"bulb","theme":"outlined"},
  {"name":"caret-right-filled","component":"CaretRightFilled","sourceName":"caret-right","theme":"filled"},
  {"name":"close-outlined","component":"CloseOutlined","sourceName":"close","theme":"outlined"},
  {"name":"code-filled","component":"CodeFilled","sourceName":"code","theme":"filled"},
  {"name":"dashboard-outlined","component":"DashboardOutlined","sourceName":"dashboard","theme":"outlined"},
  {"name":"field-binary-outlined","component":"FieldBinaryOutlined","sourceName":"field-binary","theme":"outlined"},
  {"name":"field-number-outlined","component":"FieldNumberOutlined","sourceName":"field-number","theme":"outlined"},
  {"name":"file-exclamation-filled","component":"FileExclamationFilled","sourceName":"file-exclamation","theme":"filled"},
  {"name":"file-filled","component":"FileFilled","sourceName":"file","theme":"filled"},
  {"name":"folder-add-filled","component":"FolderAddFilled","sourceName":"folder-add","theme":"filled"},
  {"name":"fullscreen-outlined","component":"FullscreenOutlined","sourceName":"fullscreen","theme":"outlined"},
  {"name":"hourglass-outlined","component":"HourglassOutlined","sourceName":"hourglass","theme":"outlined"},
  {"name":"info-circle-outlined","component":"InfoCircleOutlined","sourceName":"info-circle","theme":"outlined"},
  {"name":"insert-row-below-outlined","component":"InsertRowBelowOutlined","sourceName":"insert-row-below","theme":"outlined"},
  {"name":"loading-outlined","component":"LoadingOutlined","sourceName":"loading","theme":"outlined"},
  {"name":"picture-outlined","component":"PictureOutlined","sourceName":"picture","theme":"outlined"},
  {"name":"qq-outlined","component":"QqOutlined","sourceName":"qq","theme":"outlined"},
  {"name":"safety-certificate-outlined","component":"SafetyCertificateOutlined","sourceName":"safety-certificate","theme":"outlined"},
  {"name":"shop-outlined","component":"ShopOutlined","sourceName":"shop","theme":"outlined"},
  {"name":"truck-outlined","component":"TruckOutlined","sourceName":"truck","theme":"outlined"},
] as const) satisfies readonly IconCatalogEntry[];
export type Shard03Name = (typeof shard03Entries)[number]["name"];
