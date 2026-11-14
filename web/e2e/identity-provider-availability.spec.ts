import { execFileSync, spawn } from 'node:child_process';
import { test, expect } from '@playwright/test';
import { jwksURL } from './support/identity-provider';

// The server refuses every cloud sign-in for a while once one key fetch fails, and it gives a fetch five
// seconds. The test workers run the CLI synchronously, so the stand-in must not depend on any of them.
test('identity provider: the JWKS answers within a second while a worker runs a synchronous CLI call', async () => {
  const fetchLater = spawn(process.execPath, ['-e', `
    setTimeout(async () => {
      const started = Date.now();
      const response = await fetch(${JSON.stringify(jwksURL)});
      await response.json();
      process.stdout.write(String(Date.now() - started));
    }, 500);
  `], { stdio: ['ignore', 'pipe', 'inherit'] });
  const elapsed = new Promise<number>((resolve, reject) => {
    let out = '';
    fetchLater.stdout.on('data', (chunk) => { out += chunk; });
    fetchLater.once('error', reject);
    fetchLater.once('close', (code) => (code === 0 ? resolve(Number(out)) : reject(new Error(`fetch process exited with ${code}`))));
  });

  // The whole fetch happens while this process sits in a blocking call, as it does in mustColliedesk.
  execFileSync('sleep', ['3']);

  expect(await elapsed).toBeLessThan(1000);
});
