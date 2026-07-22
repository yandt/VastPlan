import { createHash, randomBytes } from "node:crypto";

import { JWKSVerifier } from "./jwt.mjs";

const randomId = () => randomBytes(24).toString("base64url");
const localized = (zh, en) => ({ "zh-CN": zh, "en-US": en });
const rejected = (reasonCode = "authentication.invalid_credentials") => ({
  result: { state: "rejected", reasonCode },
});

export class OIDCProvider {
  constructor(
    profiles,
    { fetchImpl = fetch, now = () => Date.now(), verifier, materialLease } = {},
  ) {
    this.profiles = profiles;
    this.fetch = fetchImpl;
    this.now = now;
    this.verifier = verifier ?? new JWKSVerifier(fetchImpl, now);
    this.materialLease = materialLease;
    this.transactions = new Map();
  }

  describe() {
    return {
      protocol: "authentication.method.v1",
      methods: [
        {
          methodId: "oidc",
          providerId: "enterprise-oidc",
          kind: "redirect",
          interaction: "redirect",
          displayName: localized("企业统一登录", "Enterprise single sign-on"),
          amr: ["oidc"],
          acr: "oidc",
          supportsResend: false,
        },
      ],
    };
  }

  begin(request) {
    const profile = this.profiles.get(request.providerProfileId);
    if (!profile || request.methodId !== "oidc")
      return rejected("authentication.method_unavailable");
    this.#prune();
    if (this.transactions.size >= 4096)
      return rejected("authentication.rate_limited");
    const state = randomId();
    const nonce = randomId();
    const verifier = randomBytes(32).toString("base64url");
    const challenge = createHash("sha256").update(verifier).digest("base64url");
    const stepId = randomId();
    const expiresAt = new Date(this.now() + 5 * 60_000);
    this.transactions.set(request.transactionId, {
      profile,
      tenantId: request.tenantId,
      state,
      nonce,
      verifier,
      stepId,
      expiresAt: expiresAt.getTime(),
    });
    const url = new URL(profile.authorizationEndpoint);
    for (const [key, value] of Object.entries({
      response_type: "code",
      client_id: profile.clientId,
      redirect_uri: profile.redirectUri,
      scope: profile.scopes.join(" "),
      state,
      nonce,
      code_challenge: challenge,
      code_challenge_method: "S256",
    }))
      url.searchParams.set(key, value);
    return {
      result: {
        state: "challenge",
        step: {
          stepId,
          kind: "redirect",
          title: localized("企业统一登录", "Enterprise single sign-on"),
          description: localized(
            "继续前往企业身份服务",
            "Continue to your enterprise identity provider",
          ),
          submitLabel: localized("继续", "Continue"),
          fields: [],
          redirectUri: url.toString(),
          expiresAt: expiresAt.toISOString(),
        },
      },
    };
  }

  async continue(request, signal) {
    const transaction = this.transactions.get(request.transactionId);
    this.transactions.delete(request.transactionId);
    if (
      !transaction ||
      transaction.stepId !== request.stepId ||
      transaction.expiresAt <= this.now()
    )
      return {
        result: {
          state: "expired",
          reasonCode: "authentication.transaction_invalid",
        },
      };
    const callback = request.redirect;
    if (
      !callback ||
      callback.state !== transaction.state ||
      callback.error ||
      !callback.code
    )
      return rejected("authentication.challenge_rejected");
    const exchange = async (secret) => {
      const values = {
        grant_type: "authorization_code",
        code: callback.code,
        redirect_uri: transaction.profile.redirectUri,
        client_id: transaction.profile.clientId,
        code_verifier: transaction.verifier,
      };
      if (secret !== undefined) values.client_secret = secret.toString("utf8");
      return this.fetch(transaction.profile.tokenEndpoint, {
        method: "POST",
        headers: {
          accept: "application/json",
          "content-type": "application/x-www-form-urlencoded",
        },
        body: new URLSearchParams(values),
        signal,
        redirect: "error",
      });
    };
    const response =
      transaction.profile.clientSecretRef === undefined
        ? await exchange()
        : await this.materialLease.withMaterial(
            transaction.profile.clientSecretRef,
            transaction.tenantId,
            signal,
            exchange,
          );
    if (!response.ok) return rejected();
    const tokens = await response.json();
    const claims = await this.verifier.verify(
      tokens.id_token,
      transaction.profile,
      transaction.nonce,
      signal,
    );
    const now = new Date(this.now());
    return {
      result: {
        state: "authenticated",
        evidence: {
          evidenceId: `oidc.${randomId()}`,
          transactionId: request.transactionId,
          methodId: "oidc",
          providerId: "enterprise-oidc",
          subject: {
            id: String(claims.sub),
            issuer: transaction.profile.issuer,
          },
          amr:
            Array.isArray(claims.amr) && claims.amr.length
              ? [...new Set(claims.amr.map(String))]
              : ["oidc"],
          acr: String(claims.acr ?? transaction.profile.acr),
          authenticatedAt: now.toISOString(),
          expiresAt: new Date(now.getTime() + 30_000).toISOString(),
          nonce: randomId(),
        },
      },
    };
  }

  cancel(request) {
    this.transactions.delete(request.transactionId);
    return { cancelled: true };
  }
  health() {
    return { ready: this.profiles.size > 0, providerId: "enterprise-oidc" };
  }
  #prune() {
    const now = this.now();
    for (const [id, value] of this.transactions)
      if (value.expiresAt <= now) this.transactions.delete(id);
  }
}
