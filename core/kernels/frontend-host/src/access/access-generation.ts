import { createHash } from "node:crypto";
import type { AccessBranding, AccessLocalizationPolicy, AccessMethodPolicy, AccessProfile } from "./access-profile-contract";

export interface AccessBootstrap {
  readonly schemaVersion: "v1";
  readonly generationId: string;
  readonly accessTemplate: string;
  readonly localization: AccessLocalizationPolicy;
  readonly authentication: AccessMethodPolicy;
  readonly branding: Omit<AccessBranding, "logoSha256">;
}

export interface AccessGeneration {
  readonly id: string;
  readonly profile: AccessProfile;
  readonly bootstrap: AccessBootstrap;
}

export function createAccessGeneration(profile: AccessProfile): AccessGeneration {
  const id = createHash("sha256").update(canonicalJSON(profile)).digest("hex");
  const branding = {
    productName: profile.branding.productName,
    ...(profile.branding.logoAssetId === undefined ? {} : { logoAssetId: profile.branding.logoAssetId }),
    ...(profile.branding.supportPath === undefined ? {} : { supportPath: profile.branding.supportPath }),
    ...(profile.branding.privacyPath === undefined ? {} : { privacyPath: profile.branding.privacyPath }),
  };
  const bootstrap: AccessBootstrap = Object.freeze({
    schemaVersion: "v1",
    generationId: id,
    accessTemplate: profile.accessTemplate,
    localization: profile.localization,
    authentication: profile.authentication,
    branding: Object.freeze(branding),
  });
  return Object.freeze({ id, profile, bootstrap });
}

function canonicalJSON(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonicalJSON).join(",")}]`;
  if (typeof value === "object" && value !== null) {
    const record = value as Record<string, unknown>;
    return `{${Object.keys(record).sort().map((key) => `${JSON.stringify(key)}:${canonicalJSON(record[key])}`).join(",")}}`;
  }
  return JSON.stringify(value);
}
