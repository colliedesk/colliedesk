// Local preview of _site/ with Cloudflare Pages URL rules (/pricing -> pricing.html).
import { createServer } from 'node:http';
import { existsSync, readFileSync, statSync } from 'node:fs';
import { extname, join, normalize } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('../_site/', import.meta.url));
const port = Number(process.env.PORT || 4321);
const TYPES = {
  '.html': 'text/html; charset=utf-8', '.css': 'text/css; charset=utf-8', '.svg': 'image/svg+xml',
  '.md': 'text/markdown; charset=utf-8', '.txt': 'text/plain; charset=utf-8', '.xml': 'application/xml; charset=utf-8',
};

function resolve(urlPath) {
  const clean = normalize(decodeURIComponent(urlPath.split('?')[0])).replace(/^(\.\.[/\\])+/, '');
  const base = join(root, clean);
  if (!base.startsWith(root.slice(0, -1))) return null;
  const candidates = clean.endsWith('/') ? [join(base, 'index.html')] : [base, `${base}.html`, join(base, 'index.html')];
  return candidates.find((c) => existsSync(c) && statSync(c).isFile()) ?? null;
}

createServer((req, res) => {
  const file = resolve(req.url);
  const status = file ? 200 : 404;
  const body = readFileSync(file ?? join(root, '404.html'));
  res.writeHead(status, { 'Content-Type': TYPES[extname(file ?? '.html')] || 'application/octet-stream' });
  res.end(body);
}).listen(port, () => console.log(`serving _site/ on http://localhost:${port}`));
