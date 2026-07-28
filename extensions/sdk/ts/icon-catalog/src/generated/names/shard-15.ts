import type { IconCatalogEntry } from "../../types.js";

export const shard15Entries = Object.freeze([
  {"name":"account-book-two-tone","component":"AccountBookTwoTone","sourceName":"account-book","theme":"twotone"},
  {"name":"camera-filled","component":"CameraFilled","sourceName":"camera","theme":"filled"},
  {"name":"ci-outlined","component":"CiOutlined","sourceName":"ci","theme":"outlined"},
  {"name":"control-two-tone","component":"ControlTwoTone","sourceName":"control","theme":"twotone"},
  {"name":"dingding-outlined","component":"DingdingOutlined","sourceName":"dingding","theme":"outlined"},
  {"name":"flag-filled","component":"FlagFilled","sourceName":"flag","theme":"filled"},
  {"name":"github-filled","component":"GithubFilled","sourceName":"github","theme":"filled"},
  {"name":"history-outlined","component":"HistoryOutlined","sourceName":"history","theme":"outlined"},
  {"name":"medicine-box-outlined","component":"MedicineBoxOutlined","sourceName":"medicine-box","theme":"outlined"},
  {"name":"plus-circle-filled","component":"PlusCircleFilled","sourceName":"plus-circle","theme":"filled"},
  {"name":"property-safety-two-tone","component":"PropertySafetyTwoTone","sourceName":"property-safety","theme":"twotone"},
  {"name":"radius-bottomleft-outlined","component":"RadiusBottomleftOutlined","sourceName":"radius-bottomleft","theme":"outlined"},
  {"name":"safety-certificate-two-tone","component":"SafetyCertificateTwoTone","sourceName":"safety-certificate","theme":"twotone"},
  {"name":"shop-filled","component":"ShopFilled","sourceName":"shop","theme":"filled"},
  {"name":"snippets-outlined","component":"SnippetsOutlined","sourceName":"snippets","theme":"outlined"},
  {"name":"tags-filled","component":"TagsFilled","sourceName":"tags","theme":"filled"},
  {"name":"twitch-outlined","component":"TwitchOutlined","sourceName":"twitch","theme":"outlined"},
  {"name":"underline-outlined","component":"UnderlineOutlined","sourceName":"underline","theme":"outlined"},
  {"name":"wallet-two-tone","component":"WalletTwoTone","sourceName":"wallet","theme":"twotone"},
  {"name":"weibo-circle-outlined","component":"WeiboCircleOutlined","sourceName":"weibo-circle","theme":"outlined"},
] as const) satisfies readonly IconCatalogEntry[];
export type Shard15Name = (typeof shard15Entries)[number]["name"];
