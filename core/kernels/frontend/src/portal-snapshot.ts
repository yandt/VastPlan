import type { PortalSpec, ShellSelection } from "./portal-contracts";

export function snapshotPortal(portal: PortalSpec): Readonly<PortalSpec> {
  const services = portal.management.services.map((service) => Object.freeze({
    ...service,
    capabilities: Object.freeze(service.capabilities.map((grant) => Object.freeze({
      ...grant,
      read: grant.read === undefined ? undefined : Object.freeze([...grant.read]),
      write: grant.write === undefined ? undefined : Object.freeze([...grant.write]),
    }))),
  }));
  return Object.freeze({
    ...portal,
    experience: portal.experience === undefined ? undefined : Object.freeze({ permissions: Object.freeze([...portal.experience.permissions]) }),
    branding: portal.branding === undefined ? undefined : freezeJSONRecord(portal.branding),
    localization: portal.localization === undefined ? undefined : Object.freeze({
      defaultLocale: portal.localization.defaultLocale,
      supportedLocales: Object.freeze([...portal.localization.supportedLocales]),
    }),
    runtimeEngine: Object.freeze({ ...portal.runtimeEngine }),
    renderAdapter: Object.freeze({
      ...portal.renderAdapter,
      config: Object.freeze({
        ...portal.renderAdapter.config,
        allowedRenderers: Object.freeze([...portal.renderAdapter.config.allowedRenderers]),
        rendererOptions: portal.renderAdapter.config.rendererOptions === undefined ? undefined : Object.freeze(Object.fromEntries(
          Object.entries(portal.renderAdapter.config.rendererOptions).map(([renderer, options]) => [renderer, Object.freeze({ ...options })]),
        )),
      }),
    }),
    shell: Object.freeze({
      ...portal.shell,
      config: Object.freeze({
        ...portal.shell.config,
        navigationGroups: portal.shell.config.navigationGroups === undefined ? undefined : Object.freeze(
          portal.shell.config.navigationGroups.map((group) => freezeJSONRecord(group)),
        ),
        allowedTemplates: Object.freeze([...portal.shell.config.allowedTemplates]),
        templateOptions: portal.shell.config.templateOptions === undefined ? undefined : Object.freeze(Object.fromEntries(
          Object.entries(portal.shell.config.templateOptions).map(([template, options]) => [template, freezeJSONRecord(options)]),
        )),
      }) as ShellSelection["config"],
    }),
    workbench: Object.freeze({
      ...portal.workbench,
      config: portal.workbench.config === undefined ? undefined : freezeJSONRecord(portal.workbench.config),
    }),
    plugins: Object.freeze(portal.plugins.map((ref) => Object.freeze({ ...ref }))),
    management: Object.freeze({ ...portal.management, services: Object.freeze(services) }),
    resolution: Object.freeze({ ...portal.resolution, pluginOrigins: Object.freeze({ ...portal.resolution.pluginOrigins }) }),
  });
}

function freezeJSONRecord(value: Readonly<Record<string, unknown>>): Readonly<Record<string, unknown>> {
  const copy: Record<string, unknown> = {};
  for (const [key, item] of Object.entries(value)) copy[key] = freezeJSONValue(item);
  return Object.freeze(copy);
}

function freezeJSONValue(value: unknown): unknown {
  if (Array.isArray(value)) return Object.freeze(value.map(freezeJSONValue));
  if (typeof value === "object" && value !== null) return freezeJSONRecord(value as Record<string, unknown>);
  return value;
}
