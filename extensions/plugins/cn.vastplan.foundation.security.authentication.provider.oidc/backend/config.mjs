const httpsUrl = (value, name) => {
  const parsed = new URL(value);
  if (
    parsed.protocol !== "https:" ||
    parsed.username ||
    parsed.password ||
    parsed.hash
  ) {
    throw new Error(`${name} 必须是无凭据 HTTPS URL`);
  }
  return parsed.toString();
};

export function loadConfiguration(
  raw = process.env.VASTPLAN_PLUGIN_CONFIG_JSON ?? "{}",
) {
  const document = JSON.parse(raw);
  if (!document || typeof document !== "object" || Array.isArray(document))
    throw new Error("OIDC 配置必须是对象");
  const allowedRoot = new Set(["profiles"]);
  for (const key of Object.keys(document))
    if (!allowedRoot.has(key)) throw new Error(`未知 OIDC 配置字段 ${key}`);
  if (
    !document.profiles ||
    typeof document.profiles !== "object" ||
    Array.isArray(document.profiles)
  )
    throw new Error("OIDC profiles 不能为空");
  const profiles = new Map();
  for (const [id, value] of Object.entries(document.profiles)) {
    const allowed = new Set([
      "issuer",
      "clientId",
      "clientSecretRef",
      "authorizationEndpoint",
      "tokenEndpoint",
      "jwksUri",
      "redirectUri",
      "scopes",
      "acr",
    ]);
    for (const key of Object.keys(value ?? {}))
      if (!allowed.has(key))
        throw new Error(`OIDC Profile ${id} 存在未知字段 ${key}`);
    if (!id || !value?.clientId)
      throw new Error(`OIDC Profile ${id} 缺少 clientId`);
    const scopes = [...new Set(value.scopes ?? ["openid", "profile", "email"])];
    if (
      !scopes.includes("openid") ||
      scopes.length > 16 ||
      scopes.some((scope) => !/^[A-Za-z0-9._:-]{1,80}$/.test(scope))
    )
      throw new Error(`OIDC Profile ${id} scopes 无效`);
    profiles.set(
      id,
      Object.freeze({
        id,
        issuer: httpsUrl(value.issuer, "issuer").replace(/\/$/, ""),
        clientId: String(value.clientId),
        ...(value.clientSecretRef === undefined
          ? {}
          : {
              clientSecretRef: validateCredentialRef(value.clientSecretRef, id),
            }),
        authorizationEndpoint: httpsUrl(
          value.authorizationEndpoint,
          "authorizationEndpoint",
        ),
        tokenEndpoint: httpsUrl(value.tokenEndpoint, "tokenEndpoint"),
        jwksUri: httpsUrl(value.jwksUri, "jwksUri"),
        redirectUri: httpsUrl(value.redirectUri, "redirectUri"),
        scopes,
        acr: String(value.acr ?? "oidc"),
      }),
    );
  }
  if (profiles.size === 0 || profiles.size > 64)
    throw new Error("OIDC profiles 数量必须为 1-64");
  return profiles;
}

function validateCredentialRef(value, profileId) {
  const keys = Object.keys(value ?? {});
  if (
    keys.some(
      (key) =>
        !["handle", "scope", "owner", "purpose", "version"].includes(key),
    ) ||
    !String(value?.handle).startsWith("credential://managed/") ||
    value?.scope !== "tenant" ||
    !value?.owner ||
    !value?.purpose ||
    !Number.isSafeInteger(value?.version) ||
    value.version < 1
  )
    throw new Error(`OIDC Profile ${profileId} clientSecretRef 无效`);
  return Object.freeze({
    handle: String(value.handle),
    scope: "tenant",
    owner: String(value.owner),
    purpose: String(value.purpose),
    version: Number(value.version),
  });
}
