import type { IconCatalogEntry } from "../../types.js";

export const shard12Entries = Object.freeze([
  {"name":"calculator-filled","component":"CalculatorFilled","sourceName":"calculator","theme":"filled"},
  {"name":"calendar-filled","component":"CalendarFilled","sourceName":"calendar","theme":"filled"},
  {"name":"calendar-outlined","component":"CalendarOutlined","sourceName":"calendar","theme":"outlined"},
  {"name":"car-outlined","component":"CarOutlined","sourceName":"car","theme":"outlined"},
  {"name":"caret-left-outlined","component":"CaretLeftOutlined","sourceName":"caret-left","theme":"outlined"},
  {"name":"cloud-two-tone","component":"CloudTwoTone","sourceName":"cloud","theme":"twotone"},
  {"name":"down-outlined","component":"DownOutlined","sourceName":"down","theme":"outlined"},
  {"name":"file-pdf-outlined","component":"FilePdfOutlined","sourceName":"file-pdf","theme":"outlined"},
  {"name":"file-pdf-two-tone","component":"FilePdfTwoTone","sourceName":"file-pdf","theme":"twotone"},
  {"name":"gold-filled","component":"GoldFilled","sourceName":"gold","theme":"filled"},
  {"name":"gold-outlined","component":"GoldOutlined","sourceName":"gold","theme":"outlined"},
  {"name":"left-square-outlined","component":"LeftSquareOutlined","sourceName":"left-square","theme":"outlined"},
  {"name":"medium-workmark-outlined","component":"MediumWorkmarkOutlined","sourceName":"medium-workmark","theme":"outlined"},
  {"name":"merge-cells-outlined","component":"MergeCellsOutlined","sourceName":"merge-cells","theme":"outlined"},
  {"name":"moon-outlined","component":"MoonOutlined","sourceName":"moon","theme":"outlined"},
  {"name":"pay-circle-filled","component":"PayCircleFilled","sourceName":"pay-circle","theme":"filled"},
  {"name":"play-square-outlined","component":"PlaySquareOutlined","sourceName":"play-square","theme":"outlined"},
  {"name":"python-outlined","component":"PythonOutlined","sourceName":"python","theme":"outlined"},
  {"name":"schedule-two-tone","component":"ScheduleTwoTone","sourceName":"schedule","theme":"twotone"},
  {"name":"sliders-outlined","component":"SlidersOutlined","sourceName":"sliders","theme":"outlined"},
  {"name":"star-outlined","component":"StarOutlined","sourceName":"star","theme":"outlined"},
  {"name":"taobao-square-filled","component":"TaobaoSquareFilled","sourceName":"taobao-square","theme":"filled"},
  {"name":"vertical-left-outlined","component":"VerticalLeftOutlined","sourceName":"vertical-left","theme":"outlined"},
  {"name":"whats-app-outlined","component":"WhatsAppOutlined","sourceName":"whats-app","theme":"outlined"},
] as const) satisfies readonly IconCatalogEntry[];
export type Shard12Name = (typeof shard12Entries)[number]["name"];
