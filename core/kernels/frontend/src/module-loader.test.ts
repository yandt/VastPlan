import { describe, expect, it, vi } from "vitest";
import { ModuleLoadError, VerifiedFrontendPluginLoader, parsePortalRuntimeSpec, type FrontendModuleDescriptor } from "./module-loader";

const ref = { id: "com.vastplan.platform.configuration.portal-composer", version: "1.0.0" };
const source = new TextEncoder().encode("export default { register() {} }");

async function descriptor(overrides: Partial<FrontendModuleDescriptor> = {}): Promise<FrontendModuleDescriptor> {
  const digest = await crypto.subtle.digest("SHA-256", source);
  const sha256 = [...new Uint8Array(digest)].map((value) => value.toString(16).padStart(2, "0")).join("");
  return {
    ...ref,
    entry: "frontend/dist/index.js",
    url: `/v1/portal-modules/7/${sha256}.js`,
    sha256,
    packageSha256: "a".repeat(64),
    ...overrides,
  };
}

describe("VerifiedFrontendPluginLoader", () => {
  it("imports only digest-bound descriptors and creates host provenance", async () => {
    const locked = await descriptor();
    const register = vi.fn();
    const fetcher = vi.fn(async () => new Response(source, { status: 200, headers: { "X-VastPlan-Module-SHA256": locked.sha256 } }));
    const importer = vi.fn(async () => ({ default: { register, provenance: { signed: false } } }));
    const loader = new VerifiedFrontendPluginLoader([locked], fetcher, importer);

    const loaded = await loader.load(ref);
    expect(loaded.provenance).toEqual({ signed: true, firstParty: true, integrity: `sha256:${locked.sha256}` });
    expect(loaded.register).toBeTypeOf("function");
    expect(fetcher).toHaveBeenCalledWith(locked.url, { credentials: "same-origin", cache: "force-cache" });
    expect(importer).toHaveBeenCalledOnce();
  });

  it("fails closed before import when bytes do not match the runtime lock", async () => {
    const locked = await descriptor({ sha256: "b".repeat(64), url: `/v1/portal-modules/7/${"b".repeat(64)}.js` });
    const importer = vi.fn();
    const loader = new VerifiedFrontendPluginLoader([locked], async () => new Response(source, { status: 200 }), importer);
    await expect(loader.load(ref)).rejects.toMatchObject({ code: "MODULE_INTEGRITY_MISMATCH" } satisfies Partial<ModuleLoadError>);
    expect(importer).not.toHaveBeenCalled();
  });

  it("rejects modules absent from the Edge-issued lock", async () => {
    const loader = new VerifiedFrontendPluginLoader([await descriptor()], async () => new Response(source), async () => ({}));
    await expect(loader.load({ id: "com.vastplan.product.other", version: "1.0.0" })).rejects.toMatchObject({ code: "MODULE_NOT_LOCKED" } satisfies Partial<ModuleLoadError>);
  });

  it("validates the bootstrap document before constructing a loader", async () => {
    const locked = await descriptor();
    expect(parsePortalRuntimeSpec({ portal: { revision: 7 }, modules: [locked] }).modules).toHaveLength(1);
    expect(() => parsePortalRuntimeSpec({ portal: {}, modules: [{ ...locked, url: "https://attacker.invalid/module.js" }] })).toThrowError(ModuleLoadError);
  });

  it("accepts only governed active or recovery module URLs", async () => {
    const base = await descriptor();
    const locked = await descriptor({ url: `/v1/portal-recovery-modules/8/7/${base.sha256}.js` });
    expect(parsePortalRuntimeSpec({ portal: { revision: 7 }, modules: [locked] }).modules[0].url).toBe(locked.url);
    expect(() => parsePortalRuntimeSpec({ portal: {}, modules: [{ ...locked, url: "/assets/history/module.js" }] })).toThrowError(ModuleLoadError);
  });
});
