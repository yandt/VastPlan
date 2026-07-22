import Ajv2020, { type ValidateFunction } from "ajv/dist/2020.js";
import accessSchema from "../../../../../contracts/schemas/authentication/v1/vastplan.access-profile.schema.json";
import authenticationTypesSchema from "../../../../../contracts/schemas/authentication/v1/vastplan.authentication-types.schema.json";
import compositionCommonSchema from "../../../../../contracts/schemas/composition/common/v1/vastplan.composition-common.schema.json";

export interface AccessLocalizationPolicy {
  readonly defaultLocale: string;
  readonly supportedLocales: readonly string[];
}

export interface AccessMethodPolicy {
  readonly allowedMethods: readonly string[];
  readonly defaultMethod: string;
  readonly reuseIdentifier: boolean;
}

export interface AccessBranding {
  readonly productName: Readonly<Record<string, string>>;
  readonly logoAssetId?: string;
  readonly logoSha256?: string;
  readonly supportPath?: string;
  readonly privacyPath?: string;
}

interface CompositionRef {
  readonly id: string;
  readonly revision: number;
  readonly digest: string;
}

export interface AccessProfile {
  readonly version: 1;
  readonly revision: number;
  readonly id: string;
  readonly tenantId: string;
  readonly portalId: string;
  readonly route: string;
  readonly domains: readonly string[];
  readonly platformProfile: CompositionRef;
  readonly accessTemplate: string;
  readonly localization: AccessLocalizationPolicy;
  readonly authentication: AccessMethodPolicy;
  readonly branding: AccessBranding;
}

export interface AccessProfileCatalog {
  readonly version: 1;
  readonly revision: number;
  readonly id: string;
  readonly profiles: readonly AccessProfile[];
}

const validateCatalog = createCatalogValidator();

export function parseAccessProfileCatalog(raw: string): AccessProfileCatalog {
  let value: unknown;
  try { value = JSON.parse(raw); }
  catch { throw new Error("Access Profile Catalog JSON 格式无效"); }
  if (!validateCatalog(value)) throw new Error("Access Profile Catalog 不符合公共 Schema");
  const catalog = normalizeCatalog(value);
  validateSemantics(catalog);
  return deepFreeze(catalog);
}

function createCatalogValidator(): ValidateFunction<AccessProfileCatalog> {
  const ajv = new Ajv2020({ allErrors: true, strict: true });
  ajv.addSchema(compositionCommonSchema);
  ajv.addSchema(authenticationTypesSchema);
  ajv.addSchema(accessSchema);
  const validator = ajv.getSchema<AccessProfileCatalog>(`${accessSchema.$id}#/$defs/accessProfileCatalog`);
  if (validator === undefined) throw new Error("Access Profile Catalog Schema 未登记");
  return validator;
}

function normalizeCatalog(value: AccessProfileCatalog): AccessProfileCatalog {
  const profiles = value.profiles.map((profile) => ({
    ...profile,
    domains: [...profile.domains].map((domain) => domain.toLowerCase().replace(/\.$/, "")).sort(),
    localization: { ...profile.localization, supportedLocales: [...profile.localization.supportedLocales] },
    authentication: { ...profile.authentication, allowedMethods: [...profile.authentication.allowedMethods] },
    branding: { ...profile.branding, productName: { ...profile.branding.productName } },
    platformProfile: { ...profile.platformProfile },
  })).sort((left, right) => left.id.localeCompare(right.id));
  return { ...value, profiles };
}

function validateSemantics(catalog: AccessProfileCatalog): void {
  const profileIDs = new Set<string>();
  const routes = new Set<string>();
  for (const profile of catalog.profiles) {
    if (profileIDs.has(profile.id)) throw new Error(`Access Profile ID 重复: ${profile.id}`);
    profileIDs.add(profile.id);
    if (!isCanonicalRoute(profile.route)) throw new Error(`Access Profile route 无效: ${profile.id}`);
    if (!profile.authentication.allowedMethods.includes(profile.authentication.defaultMethod)) {
      throw new Error(`Access Profile defaultMethod 未被允许: ${profile.id}`);
    }
    if (!profile.localization.supportedLocales.some((locale) => locale.toLowerCase() === profile.localization.defaultLocale.toLowerCase())) {
      throw new Error(`Access Profile defaultLocale 未被支持: ${profile.id}`);
    }
    if ((profile.branding.logoAssetId === undefined) !== (profile.branding.logoSha256 === undefined)) {
      throw new Error(`Access Profile Logo 引用不完整: ${profile.id}`);
    }
    for (const path of [profile.branding.supportPath, profile.branding.privacyPath]) {
      if (path !== undefined && !isSafeLocalPath(path)) throw new Error(`Access Profile 品牌路径无效: ${profile.id}`);
    }
    for (const domain of profile.domains) {
      const key = `${domain}\0${profile.route}`;
      if (routes.has(key)) throw new Error(`Access Profile 路由冲突: ${domain}${profile.route}`);
      routes.add(key);
    }
  }
}

function isCanonicalRoute(value: string): boolean {
  return isSafeLocalPath(value) && !/[?#]/.test(value) && (value === "/" || !value.endsWith("/"));
}

function isSafeLocalPath(value: string): boolean {
  return value.startsWith("/") && !value.startsWith("//") && !value.includes("\\") && !/[\0\r\n]/.test(value);
}

function deepFreeze<T>(value: T): T {
  if (typeof value !== "object" || value === null || Object.isFrozen(value)) return value;
  for (const nested of Object.values(value)) deepFreeze(nested);
  return Object.freeze(value);
}
