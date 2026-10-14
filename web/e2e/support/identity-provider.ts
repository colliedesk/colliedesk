import { createServer } from 'node:http';
import type { Server } from 'node:http';
import { createHash, createPrivateKey, generateKeyPairSync, sign } from 'node:crypto';
import type { KeyObject } from 'node:crypto';
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

// A local stand-in for the hosted identity provider: an RS256 key pair, its JWKS endpoint, and
// session tokens signed with it. It is the only external boundary of the cloud sign-in journey.
export const identity = {
  jwksPort: 18091,
  issuer: 'https://identity.e2e.colliedesk.test',
  authorizedParty: 'http://localhost:18084',
  publishableKey: 'pk_test_e2e_colliedesk',
} as const;

export const jwksURL = `http://127.0.0.1:${identity.jwksPort}/.well-known/jwks.json`;

const base64url = (value: Buffer | string) => Buffer.from(value).toString('base64url');

// The server caches signing keys by key id for its whole life, like a real deployment. The key is
// kept on disk so a re-loaded test file (retries, --repeat-each) signs with the key already cached.
function signingKey(): KeyObject {
  const path = join(tmpdir(), 'colliedesk-e2e-identity-key.pem');
  if (!existsSync(path)) {
    const key = generateKeyPairSync('rsa', { modulusLength: 2048 }).privateKey;
    writeFileSync(path, key.export({ format: 'pem', type: 'pkcs8' }), { mode: 0o600 });
  }
  return createPrivateKey(readFileSync(path));
}

export class IdentityProvider {
  private readonly key = signingKey();
  // The key id is the key's thumbprint, so a different key can never reuse a cached id.
  private readonly kid = createHash('sha256').update(String(this.key.export({ format: 'jwk' }).n)).digest('base64url').slice(0, 16);
  private server?: Server;

  async start(): Promise<void> {
    const publicJWK = { ...this.key.export({ format: 'jwk' }) };
    const jwk = { kty: 'RSA', n: publicJWK.n, e: publicJWK.e, kid: this.kid, alg: 'RS256', use: 'sig' };
    this.server = createServer((request, response) => {
      if (request.url !== '/.well-known/jwks.json') { response.writeHead(404).end(); return; }
      response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ keys: [jwk] }));
    });
    await new Promise<void>((resolve) => this.server?.listen(identity.jwksPort, '127.0.0.1', resolve));
  }

  async stop(): Promise<void> {
    await new Promise((resolve) => this.server?.close(resolve));
  }

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
