function nonEmpty(value) {
  return typeof value === "string" && value.trim() !== "";
}

// Keep development HMR classification aligned with Backend portaltrust. Only a
// complete, singular Renderer module or Shell Library contribution is deferred;
// malformed or mixed contributions remain eager and will be rejected later by
// the trusted Catalog instead of being granted deferred-module semantics here.
export function isDeferredFrontendContribution(frontend) {
  const renderers = frontend?.rendererModules;
  if (Array.isArray(renderers) && renderers.length === 1) {
    const renderer = renderers[0];
    if (nonEmpty(renderer?.id) && renderer.adapter === "ui.render.adapter" &&
        nonEmpty(renderer.uiContract) && nonEmpty(renderer.framework)) return true;
  }

  const libraries = frontend?.shellLibraries;
  if (Array.isArray(libraries) && libraries.length === 1) {
    const library = libraries[0];
    return nonEmpty(library?.id) && library.shell === "ui.structure.shell" && nonEmpty(library.uiContract);
  }
  return false;
}
