import type { IconCatalogEntry } from "../../types.js";

export const shard07Entries = Object.freeze([
  {"name":"api-outlined","component":"ApiOutlined","sourceName":"api","theme":"outlined"},
  {"name":"bar-chart-outlined","component":"BarChartOutlined","sourceName":"bar-chart","theme":"outlined"},
  {"name":"border-outlined","component":"BorderOutlined","sourceName":"border","theme":"outlined"},
  {"name":"container-two-tone","component":"ContainerTwoTone","sourceName":"container","theme":"twotone"},
  {"name":"deep-seek-filled","component":"DeepSeekFilled","sourceName":"deep-seek","theme":"filled"},
  {"name":"environment-outlined","component":"EnvironmentOutlined","sourceName":"environment","theme":"outlined"},
  {"name":"facebook-outlined","component":"FacebookOutlined","sourceName":"facebook","theme":"outlined"},
  {"name":"file-protect-outlined","component":"FileProtectOutlined","sourceName":"file-protect","theme":"outlined"},
  {"name":"folder-open-two-tone","component":"FolderOpenTwoTone","sourceName":"folder-open","theme":"twotone"},
  {"name":"function-outlined","component":"FunctionOutlined","sourceName":"function","theme":"outlined"},
  {"name":"harmony-o-s-outlined","component":"HarmonyOSOutlined","sourceName":"harmony-o-s","theme":"outlined"},
  {"name":"home-two-tone","component":"HomeTwoTone","sourceName":"home","theme":"twotone"},
  {"name":"insurance-two-tone","component":"InsuranceTwoTone","sourceName":"insurance","theme":"twotone"},
  {"name":"meh-outlined","component":"MehOutlined","sourceName":"meh","theme":"outlined"},
  {"name":"minus-square-filled","component":"MinusSquareFilled","sourceName":"minus-square","theme":"filled"},
  {"name":"node-expand-outlined","component":"NodeExpandOutlined","sourceName":"node-expand","theme":"outlined"},
  {"name":"open-a-i-outlined","component":"OpenAIOutlined","sourceName":"open-a-i","theme":"outlined"},
  {"name":"play-square-filled","component":"PlaySquareFilled","sourceName":"play-square","theme":"filled"},
  {"name":"product-outlined","component":"ProductOutlined","sourceName":"product","theme":"outlined"},
  {"name":"rest-outlined","component":"RestOutlined","sourceName":"rest","theme":"outlined"},
  {"name":"rotate-left-outlined","component":"RotateLeftOutlined","sourceName":"rotate-left","theme":"outlined"},
  {"name":"weibo-circle-filled","component":"WeiboCircleFilled","sourceName":"weibo-circle","theme":"filled"},
  {"name":"yahoo-outlined","component":"YahooOutlined","sourceName":"yahoo","theme":"outlined"},
] as const) satisfies readonly IconCatalogEntry[];
export type Shard07Name = (typeof shard07Entries)[number]["name"];
