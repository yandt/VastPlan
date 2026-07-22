import { createPublicKey, verify } from 'node:crypto';

const decodePart = (value) => JSON.parse(Buffer.from(value, 'base64url').toString('utf8'));

export class JWKSVerifier {
  constructor(fetchImpl = fetch, now = () => Date.now()) {
    this.fetch = fetchImpl;
    this.now = now;
    this.cache = new Map();
  }

  async keys(uri, signal) {
    const cached = this.cache.get(uri);
    if (cached && cached.expiresAt > this.now()) return cached.keys;
    const response = await this.fetch(uri, { headers: { accept: 'application/json' }, signal, redirect: 'error' });
    if (!response.ok) throw new Error('OIDC JWKS 获取失败');
    const document = await response.json();
    if (!Array.isArray(document.keys) || document.keys.length > 64) throw new Error('OIDC JWKS 响应无效');
    this.cache.set(uri, { keys: document.keys, expiresAt: this.now() + 5 * 60_000 });
    return document.keys;
  }

  async verify(token, profile, expectedNonce, signal) {
    if (typeof token !== 'string' || token.length > 32_768) throw new Error('OIDC id_token 无效');
    const parts = token.split('.');
    if (parts.length !== 3) throw new Error('OIDC id_token 格式无效');
    const header = decodePart(parts[0]);
    const claims = decodePart(parts[1]);
    if (!['RS256', 'ES256'].includes(header.alg) || !header.kid) throw new Error('OIDC 签名算法或 kid 无效');
    const keys = await this.keys(profile.jwksUri, signal);
    const jwk = keys.find((candidate) => candidate.kid === header.kid && (!candidate.alg || candidate.alg === header.alg) && (!candidate.use || candidate.use === 'sig'));
    if (!jwk) throw new Error('OIDC 签名密钥不存在');
    const key = createPublicKey({ key: jwk, format: 'jwk' });
    const options = header.alg === 'ES256' ? { key, dsaEncoding: 'ieee-p1363' } : key;
    if (!verify('sha256', Buffer.from(`${parts[0]}.${parts[1]}`), options, Buffer.from(parts[2], 'base64url'))) throw new Error('OIDC id_token 签名无效');
    const now = Math.floor(this.now() / 1000);
    const audiences = Array.isArray(claims.aud) ? claims.aud : [claims.aud];
    if (claims.iss !== profile.issuer || !audiences.includes(profile.clientId) || !claims.sub || claims.nonce !== expectedNonce) throw new Error('OIDC id_token 绑定无效');
    if (audiences.length > 1 && claims.azp !== profile.clientId) throw new Error('OIDC id_token azp 无效');
    if (!Number.isSafeInteger(claims.exp) || claims.exp < now - 60 || (claims.iat && claims.iat > now + 60)) throw new Error('OIDC id_token 已过期或时间无效');
    return claims;
  }
}
