import { Contribution, Plugin, callResult } from "@vastplan/backend-plugin";
import { MaterialLeaseClient } from "@vastplan/credential-lease-node";

import { loadConfiguration } from "./config.mjs";
import { OIDCProvider } from "./provider.mjs";

const plugin = new Plugin({
  id: "cn.vastplan.foundation.security.authentication.provider.oidc",
  version: "0.1.1",
  engines: { backend: "^0.1" },
});
const provider = new OIDCProvider(loadConfiguration(), {
  materialLease: new MaterialLeaseClient(plugin, {
    audience: process.env.VASTPLAN_RUNTIME_AUDIENCE,
  }),
});
const descriptor = {
  title: "企业 OIDC Provider",
  protocol: "authentication.method.v1",
  purposes: [
    "portal-login",
    "mobile-token",
    "runner-token",
    "token-verification",
  ],
  methods: [{ id: "oidc", kind: "redirect", interaction: "redirect" }],
  subjectNamespace: "enterprise.identity.oidc",
  requiredCapabilities: [],
};

const jsonHandler =
  (handler) => async (invocation, _host, _context, payload) => {
    invocation.throwIfCancelled();
    try {
      const result = await handler(
        JSON.parse(payload.toString() || "{}"),
        invocation.signal.signal,
      );
      return callResult.ok(Buffer.from(JSON.stringify(result)));
    } catch {
      return callResult.ok(
        Buffer.from(
          JSON.stringify({
            result: {
              state: "rejected",
              reasonCode: "authentication.challenge_rejected",
            },
          }),
        ),
      );
    }
  };

plugin.contribute(
  new Contribution({
    extensionPoint: "authentication.provider",
    id: "enterprise-oidc",
    descriptor,
    handlers: {
      describe: jsonHandler(() => provider.describe()),
      begin: jsonHandler((request) => provider.begin(request)),
      continue: jsonHandler((request, signal) =>
        provider.continue(request, signal),
      ),
      resend: jsonHandler(() => ({
        result: {
          state: "rejected",
          reasonCode: "authentication.method_unavailable",
        },
      })),
      cancel: jsonHandler((request) => provider.cancel(request)),
      health: jsonHandler(() => provider.health()),
    },
  }),
);

export const start = () => plugin.serve();
export const shutdown = () => plugin.shutdown();
export { descriptor };
