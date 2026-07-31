import { contributionsByKind, type ContributionIndexSnapshot, type IndexedPluginContribution } from "@vastplan/plugin-inventory-contract";
import type { LocalizedText, UIRenderAdapter, UIShellAdapter } from "@vastplan/ui-primitives";
import type { PluginRef, PortalSpec } from "./portal-contracts";
import { PortalAssemblyError } from "./portal-errors";
import { samePlugin } from "./portal-validation";

/** Builds exact Foundation catalogs from verified Manifest contributions. */
export function renderAdapterCatalogFromIndex(adapter: UIRenderAdapter, index: ContributionIndexSnapshot | undefined, portal: PortalSpec): UIRenderAdapter {
  if (index === undefined) return adapter;
  const renderers = contributionsByKind(index, "frontend.rendererModules")
    .map((contribution) => rendererTemplate(contribution, portal))
    .filter((value) => value !== undefined);
  assertUniqueIDs(renderers, "Renderer");
  return Object.freeze({ ...adapter, renderers: Object.freeze(renderers) });
}

export function shellCatalogFromIndex(shell: UIShellAdapter, index: ContributionIndexSnapshot | undefined, portal: PortalSpec): UIShellAdapter {
  if (index === undefined) return shell;
  const templates = contributionsByKind(index, "frontend.shellLibraries")
    .map((contribution) => shellTemplate(contribution, portal))
    .filter((value) => value !== undefined);
  assertUniqueIDs(templates, "Shell Library");
  return Object.freeze({ ...shell, templates: Object.freeze(templates) });
}

function rendererTemplate(contribution: IndexedPluginContribution, portal: PortalSpec): UIRenderAdapter["renderers"][number] | undefined {
  const descriptor = contribution.descriptor;
  if (descriptor.adapter !== "ui.render.adapter" || descriptor.uiContract !== portal.renderAdapter.uiContract || descriptor.engineFamily !== portal.runtimeEngine.family) return undefined;
  if (typeof descriptor.framework !== "string" || descriptor.framework.length === 0) throw invalidContribution(contribution);
  const module = ownerRef(contribution);
  requireLockedPlatformModule(module, contribution, portal);
  return Object.freeze({ id: contribution.id, label: contributionLabel(portal.renderAdapter.id, `renderer.${contribution.id}`, descriptor.title, descriptor.framework), framework: descriptor.framework, module: Object.freeze(module) });
}

function shellTemplate(contribution: IndexedPluginContribution, portal: PortalSpec): UIShellAdapter["templates"][number] | undefined {
  const descriptor = contribution.descriptor;
  if (descriptor.shell !== "ui.structure.shell" || descriptor.uiContract !== portal.shell.uiContract) return undefined;
  const module = ownerRef(contribution);
  requireLockedPlatformModule(module, contribution, portal);
  return Object.freeze({ id: contribution.id, label: contributionLabel(portal.shell.id, `template.${contribution.id}`, descriptor.title, contribution.id), module: Object.freeze(module) });
}

function ownerRef(contribution: IndexedPluginContribution): PluginRef {
  return { id: contribution.owner.ref.pluginId, version: contribution.owner.ref.version, channel: contribution.owner.ref.channel };
}

function requireLockedPlatformModule(module: PluginRef, contribution: IndexedPluginContribution, portal: PortalSpec): void {
  if (!portal.plugins.some((candidate) => samePlugin(candidate, module)) || portal.resolution.pluginOrigins[module.id] !== "platform-profile") {
    throw new PortalAssemblyError("FOUNDATION_CONTRIBUTION_OWNER_INVALID", `Foundation Contribution 未绑定 Platform Profile: ${contribution.kind}/${contribution.id}`);
  }
}

function contributionLabel(namespace: string, key: string, title: unknown, fallback: string): LocalizedText {
  return Object.freeze({ namespace, key, fallback: typeof title === "string" && title.length > 0 ? title : fallback });
}

function assertUniqueIDs(values: readonly { id: string }[], kind: string): void {
  if (new Set(values.map((value) => value.id)).size !== values.length) throw new PortalAssemblyError("FOUNDATION_CONTRIBUTION_AMBIGUOUS", `${kind} 候选未由 Profile 消歧`);
}

function invalidContribution(contribution: IndexedPluginContribution): PortalAssemblyError {
  return new PortalAssemblyError("FOUNDATION_CONTRIBUTION_INVALID", `Foundation Contribution 描述无效: ${contribution.kind}/${contribution.id}`);
}
