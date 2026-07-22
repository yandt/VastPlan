import assert from "node:assert/strict";
import test from "node:test";
import { isDeferredFrontendContribution } from "./frontend-plugin-contribution.mjs";

test("classifies governed Renderer modules and Shell Libraries as deferred", () => {
  assert.equal(isDeferredFrontendContribution({ rendererModules: [{
    id: "arco", adapter: "ui.render.adapter", uiContract: "^4.0.0", framework: "react",
  }] }), true);
  assert.equal(isDeferredFrontendContribution({ shellLibraries: [{
    id: "standard", shell: "ui.structure.shell", uiContract: "^4.0.0",
  }] }), true);
});

test("keeps ordinary and malformed frontend contributions eager", () => {
  assert.equal(isDeferredFrontendContribution({ views: [{ id: "settings" }] }), false);
  assert.equal(isDeferredFrontendContribution({ shellLibraries: [{ id: "standard", shell: "other", uiContract: "^4.0.0" }] }), false);
  assert.equal(isDeferredFrontendContribution({ shellLibraries: [
    { id: "standard", shell: "ui.structure.shell", uiContract: "^4.0.0" },
    { id: "top", shell: "ui.structure.shell", uiContract: "^4.0.0" },
  ] }), false);
});
