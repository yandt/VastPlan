import { describe, expect, it, vi } from "vitest";
import { computeModuleGraphDigest, VerifiedModuleGraphLoader, type FrontendModuleGraphDescriptor, type FrontendModuleNodeDescriptor } from "./module-graph-loader";
import { ownedBuffer, sha256Hex } from "./module-integrity";
import { developmentFrontendRuntimeProtocol, productionFrontendRuntimeProtocol } from "./frontend-runtime-protocol";

const ref = { id: "cn.vastplan.product.graph-test", version: "1.0.0", channel: "stable" } as const;

async function fixture(): Promise<{ graph: FrontendModuleGraphDescriptor; content: Map<string, Uint8Array> }> {
  const content = new Map([
    ["frontend/dist/main.js", new TextEncoder().encode("export const ready = import('./chunks/lazy.js');\n")],
    ["frontend/dist/chunks/lazy.js", new TextEncoder().encode("export const lazy = true;\n")],
  ]);
  const node = async (path: string, purpose: FrontendModuleNodeDescriptor["purpose"], dependencies: FrontendModuleNodeDescriptor["dependencies"]): Promise<FrontendModuleNodeDescriptor> => {
    const bytes = content.get(path)!;
    const sha256 = await sha256Hex(bytes);
    return { path, url: `/v1/portal-modules/7/${sha256}.js`, sha256, size: bytes.byteLength, mediaType: "text/javascript", purpose, dependencies };
  };
  const nodes = [
    await node("frontend/dist/main.js", "entry", [{ specifier: "./chunks/lazy.js", path: "frontend/dist/chunks/lazy.js", kind: "dynamic" }]),
    await node("frontend/dist/chunks/lazy.js", "chunk", []),
  ];
  const unsigned = { ...ref, target: "browser" as const, entry: nodes[0].path, digest: "0".repeat(64), packageSha256: "a".repeat(64), externals: ["react"], nodes };
  return { graph: { ...unsigned, digest: await computeModuleGraphDigest(unsigned) }, content };
}

describe("VerifiedModuleGraphLoader", () => {
  it("verifies the complete DAG and rewrites relative chunks to retained Blob URLs", async () => {
    const { graph, content } = await fixture();
    const blobs: Blob[] = [];
    const create = vi.spyOn(URL, "createObjectURL").mockImplementation((blob) => {
      blobs.push(blob as Blob);
      return `blob:vastplan/${blobs.length}`;
    });
    const revoke = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => undefined);
    const fetcher = vi.fn(async (url: string) => {
      const node = graph.nodes.find((candidate) => candidate.url === url)!;
      return new Response(ownedBuffer(content.get(node.path)!), { headers: { "X-VastPlan-Module-SHA256": node.sha256 } });
    });
    const importer = vi.fn(async () => ({ default: { register() {} } }));
    const loader = new VerifiedModuleGraphLoader([graph], { protocol: productionFrontendRuntimeProtocol, fetcher, importer });

    await loader.load(ref);

    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(importer).toHaveBeenCalledWith("blob:vastplan/2", graph.nodes[0].url);
    expect(await blobs[1].text()).toContain("import('blob:vastplan/1')");
    loader.dispose();
    expect(revoke.mock.calls.map(([url]) => url)).toEqual(["blob:vastplan/1", "blob:vastplan/2"]);
    create.mockRestore();
    revoke.mockRestore();
  });

  it("fails closed when one graph node has been modified", async () => {
    const { graph, content } = await fixture();
    const loader = new VerifiedModuleGraphLoader([graph], { protocol: productionFrontendRuntimeProtocol, fetcher: async (url) => {
      const node = graph.nodes.find((candidate) => candidate.url === url)!;
      const bytes = node.purpose === "entry" ? new TextEncoder().encode("x".repeat(node.size)) : content.get(node.path)!;
      return new Response(ownedBuffer(bytes));
    }, importer: async () => ({}) });
    await expect(loader.load(ref)).rejects.toMatchObject({ code: "MODULE_INTEGRITY_MISMATCH" });
  });

  it("accepts digest-bound development graph URLs only under the development policy and bypasses cache", async () => {
    const { graph, content } = await fixture();
    const developmentNodes = graph.nodes.map((node) => ({ ...node, url: `/__vastplan_dev/modules/${node.sha256}.js` }));
    const development = { ...graph, nodes: developmentNodes };
    expect(() => new VerifiedModuleGraphLoader([development], { protocol: productionFrontendRuntimeProtocol, fetcher: async () => new Response() })).toThrowError(/节点无效/);

    const fetcher = vi.fn(async (url: string) => {
      const node = development.nodes.find((candidate) => candidate.url === url)!;
      return new Response(ownedBuffer(content.get(node.path)!), { headers: { "X-VastPlan-Module-SHA256": node.sha256 } });
    });
    const loader = new VerifiedModuleGraphLoader([development], { protocol: developmentFrontendRuntimeProtocol, fetcher, importer: async () => ({ default: { register() {} } }) });
    await loader.load(ref);
    expect(fetcher).toHaveBeenCalledTimes(2);
    for (const node of development.nodes) expect(fetcher).toHaveBeenCalledWith(node.url, { credentials: "include", cache: "no-store" });
    loader.dispose();
  });

  it("keeps graph loading bound to the identity selected by the module resolver", async () => {
    const { graph, content } = await fixture();
    const unsigned = { ...graph, digest: "0".repeat(64), nodes: graph.nodes.map((node) => ({ ...node, url: `/__vastplan_dev/modules/${node.sha256}.js` })) };
    const development = { ...unsigned, digest: await computeModuleGraphDigest(unsigned) };
    const fetcher = vi.fn(async (url: string) => {
      const node = development.nodes.find((candidate) => candidate.url === url)!;
      return new Response(ownedBuffer(content.get(node.path)!), { headers: { "X-VastPlan-Module-SHA256": node.sha256 } });
    });
    const importer = vi.fn(async () => ({ default: { register() {} } }));
    const developmentLoader = new VerifiedModuleGraphLoader([development], { protocol: developmentFrontendRuntimeProtocol, fetcher, importer });
    expect(developmentLoader.resolve({ ...ref, version: "9.9.9" })).toBeUndefined();
    await developmentLoader.load(development);
    expect(importer).toHaveBeenCalledOnce();
  });

  it("rejects unknown externals and cyclic graphs before fetching", async () => {
    const { graph } = await fixture();
    expect(() => new VerifiedModuleGraphLoader([{ ...graph, externals: ["unknown-runtime"] }], { protocol: productionFrontendRuntimeProtocol, fetcher: async () => new Response() })).toThrowError(/未知共享依赖/);
    const cyclicNodes = graph.nodes.map((node) => node.purpose === "chunk" ? { ...node, dependencies: [{ specifier: "../main.js", path: graph.entry, kind: "static" as const }] } : node);
    const cyclic = { ...graph, nodes: cyclicNodes };
    expect(() => new VerifiedModuleGraphLoader([cyclic], { protocol: productionFrontendRuntimeProtocol, fetcher: async () => new Response() })).toThrowError(/循环依赖/);
  });

  it("enforces the browser dependency-depth limit", async () => {
    const { graph } = await fixture();
    const nodes: FrontendModuleNodeDescriptor[] = Array.from({ length: 65 }, (_, index) => {
      const sha256 = (index + 1).toString(16).padStart(64, "0");
      const path = `frontend/dist/node-${index}.js`;
      return {
        path, url: `/v1/portal-modules/7/${sha256}.js`, sha256, size: 1, mediaType: "text/javascript",
        purpose: index === 0 ? "entry" : "chunk",
        dependencies: index === 64 ? [] : [{ specifier: `node-${index + 1}.js`, path: `frontend/dist/node-${index + 1}.js`, kind: "static" }],
      };
    });
    expect(() => new VerifiedModuleGraphLoader([{ ...graph, entry: nodes[0].path, nodes }], { protocol: productionFrontendRuntimeProtocol, fetcher: async () => new Response() })).toThrowError(/深度/);
  });
});
