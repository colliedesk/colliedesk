import { createHash, createPrivateKey, generateKeyPairSync, sign } from 'node:crypto';
import type { KeyObject } from 'node:crypto';
import { linkSync, mkdirSync, readFileSync, unlinkSync, writeFileSync } from 'node:fs';
import { ports, scratchDir } from './checkout';
import { join } from 'node:path';

// A local stand-in for the hosted identity provider: an RS256 key pair, its JWKS endpoint, and
// session tokens signed with it. It is the only external boundary of the cloud sign-in journey.
export const identity = {
  jwksPort: ports.jwks,
  issuer: 'https://identity.e2e.colliedesk.test',
  authorizedParty: `http://localhost:${ports.cloud}`,
  publishableKey: 'pk_test_e2e_colliedesk',
} as const;

export const jwksURL = `http://127.0.0.1:${identity.jwksPort}/.well-known/jwks.json`;

const base64url = (value: Buffer | string) => Buffer.from(value).toString('base64url');

export const identityKeyFile = join(scratchDir, 'identity-key.json');

interface StoredKey { pem: string; jwk: { kty: string; n: string; e: string; kid: string; alg: string; use: string } }

// The server caches signing keys by key id for its whole life, like a real deployment. The key is kept on
// disk so a run signs with the key a server already cached, and so the JWKS process (support/jwks-server.mjs,
// a webServer in playwright.config.ts) and every worker that mints tokens read the same one. The file is
// created whole and exclusively (hard link), so a reader never sees half a key and two creators never
// leave two keys behind. playwright.config.ts calls this before it starts the JWKS process.
export function ensureIdentityKey(): void {
  mkdirSync(scratchDir, { recursive: true });
  try {
    readFileSync(identityKeyFile);
    return;
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error;
  }
  const key = generateKeyPairSync('rsa', { modulusLength: 2048 }).privateKey;
  const { n, e } = key.export({ format: 'jwk' });
  // The key id is the key's thumbprint, so a different key can never reuse a cached id.
  const kid = createHash('sha256').update(String(n)).digest('base64url').slice(0, 16);
  const stored: StoredKey = { pem: key.export({ format: 'pem', type: 'pkcs8' }).toString(), jwk: { kty: 'RSA', n: String(n), e: String(e), kid, alg: 'RS256', use: 'sig' } };
  const temporary = `${identityKeyFile}.${process.pid}`;
  writeFileSync(temporary, JSON.stringify(stored), { mode: 0o600 });
  try {
    linkSync(temporary, identityKeyFile);
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== 'EEXIST') throw error;
  } finally {
    unlinkSync(temporary);
  }
}

export class IdentityProvider {
  private readonly stored = (ensureIdentityKey(), JSON.parse(readFileSync(identityKeyFile, 'utf8')) as StoredKey);
  private readonly key = createPrivateKey(this.stored.pem);
  private readonly kid = this.stored.jwk.kid;

  token(claims: Record<string, unknown>, options: { key?: KeyObject; kid?: string } = {}): string {
    const now = Math.floor(Date.now() / 1000);
    const header = base64url(JSON.stringify({ alg: 'RS256', typ: 'JWT', kid: options.kid ?? this.kid }));
    const payload = base64url(JSON.stringify({
      iss: identity.issuer, azp: identity.authorizedParty, sub: 'user_e2e', email_verified: true,
      iat: now, nbf: now - 5, exp: now + 60, ...claims,
    }));
    const signature = sign('RSA-SHA256', Buffer.from(`${header}.${payload}`), options.key ?? this.key);
    return `${header}.${payload}.${base64url(signature)}`;
  }
}

export function otherKey(): KeyObject {
  return generateKeyPairSync('rsa', { modulusLength: 2048 }).privateKey;
}
