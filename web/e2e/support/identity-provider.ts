import { createServer } from 'node:http';
import type { Server } from 'node:http';
import { createHash, createPrivateKey, generateKeyPairSync, sign } from 'node:crypto';
import type { KeyObject } from 'node:crypto';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
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

// The server caches signing keys by key id for its whole life, like a real deployment. The key is
// kept on disk so a re-loaded test file (retries, --repeat-each) signs with the key already cached.
function signingKey(): KeyObject {
  mkdirSync(scratchDir, { recursive: true });
  const path = join(scratchDir, 'identity-key.pem');
  if (!existsSync(path)) {
    const key = generateKeyPairSync('rsa', { modulusLength: 2048 }).privateKey;
    writeFileSync(path, key.export({ format: 'pem', type: 'pkcs8' }), { mode: 0o600 });
  }
  return createPrivateKey(readFileSync(path));
}

// The JWKS server of this worker process. Every file that signs in on the cloud uses the one port of
// jwksURL, and CI runs files in parallel: a second worker finds the port taken by another's server, which
// serves the same key (the key is on disk), and uses it. A server stays up until its process ends, so no
// file's afterAll takes it away from a file another worker is still running.
let jwksServer: Server | undefined;

export class IdentityProvider {
  private readonly key = signingKey();
  // The key id is the key's thumbprint, so a different key can never reuse a cached id.
  private readonly kid = createHash('sha256').update(String(this.key.export({ format: 'jwk' }).n)).digest('base64url').slice(0, 16);

  async start(): Promise<void> {
    if (jwksServer) return;
    const publicJWK = { ...this.key.export({ format: 'jwk' }) };
    const jwk = { kty: 'RSA', n: publicJWK.n, e: publicJWK.e, kid: this.kid, alg: 'RS256', use: 'sig' };
    const server = createServer((request, response) => {
      if (request.url !== '/.well-known/jwks.json') { response.writeHead(404).end(); return; }
      response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ keys: [jwk] }));
    });
    const taken = await new Promise<boolean>((resolve, reject) => {
      server.once('error', (error: NodeJS.ErrnoException) => (error.code === 'EADDRINUSE' ? resolve(true) : reject(error)));
      server.listen(identity.jwksPort, '127.0.0.1', () => resolve(false));
    });
    if (!taken) {
      server.unref();
      jwksServer = server;
      return;
    }
    // Another worker's server: it must serve this key, or tokens signed here would not verify.
    const served = await (await fetch(jwksURL)).json() as { keys: Array<{ kid: string }> };
    if (!served.keys.some((key) => key.kid === this.kid)) throw new Error(`port ${identity.jwksPort} serves another identity provider`);
  }

  // stop keeps the server for the other files of this run (see jwksServer); the process ending closes it.
  async stop(): Promise<void> {}

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
