// The JWKS endpoint of the e2e identity provider stand-in, as a process of its own (a webServer in
// playwright.config.ts). The server under test fetches keys with a five second timeout; a test worker
// that sits in a synchronous CLI call answers nothing, so no worker may serve this.
// Usage: node jwks-server.mjs <identity-key.json> <port>
import { createServer } from 'node:http';
import { readFileSync } from 'node:fs';

const [file, port] = process.argv.slice(2);
const body = JSON.stringify({ keys: [JSON.parse(readFileSync(file, 'utf8')).jwk] });

createServer((request, response) => {
  if (request.url !== '/.well-known/jwks.json') { response.writeHead(404).end(); return; }
  response.writeHead(200, { 'Content-Type': 'application/json' }).end(body);
}).listen(Number(port), '127.0.0.1');
