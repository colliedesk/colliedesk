import { readFile, readdir } from 'node:fs/promises';
import path from 'node:path';
import { pathToFileURL } from 'node:url';
import { gzipSync } from 'node:zlib';

const JS_LIMIT = 200 * 1024;
const CSS_LIMIT = 30 * 1024;
const defaultBuildDirectory = path.resolve(process.cwd(), '../server/internal/webui/static/build');

async function largestGzipAsset(assetDirectory, extension) {
  const names = (await readdir(assetDirectory)).filter((name) => name.endsWith(extension));
  if (names.length === 0) {
    throw new Error(`No ${extension} asset found in ${assetDirectory}`);
  }
  const sizes = await Promise.all(
    names.map(async (name) => ({
      name,
      bytes: gzipSync(await readFile(path.join(assetDirectory, name))).byteLength,
    })),
  );
  return sizes.reduce((largest, asset) => (asset.bytes > largest.bytes ? asset : largest));
}

export async function checkBundleBudget(buildDirectory = defaultBuildDirectory) {
  const assetDirectory = path.join(buildDirectory, 'assets');
  const [javascript, css] = await Promise.all([
    largestGzipAsset(assetDirectory, '.js'),
    largestGzipAsset(assetDirectory, '.css'),
  ]);
  if (javascript.bytes > JS_LIMIT) {
    throw new Error(`JS gzip budget exceeded: ${javascript.name} is ${javascript.bytes} bytes; limit is ${JS_LIMIT}`);
  }
  if (css.bytes > CSS_LIMIT) {
    throw new Error(`CSS gzip budget exceeded: ${css.name} is ${css.bytes} bytes; limit is ${CSS_LIMIT}`);
  }
  process.stdout.write(
    `bundle budget passed: JS ${javascript.bytes}/${JS_LIMIT} bytes, CSS ${css.bytes}/${CSS_LIMIT} bytes gzip\n`,
  );
}

// The operator console is its own build (build/operator). Its first bundle (the assets operator.html loads) has its
// own ceilings: the first measurement plus 5% (docs/internals/performance.md › Operator console bundle). Never raise
// them to pass: move a screen into a lazy part instead.
export const OPERATOR_JS_LIMIT = 78652;
export const OPERATOR_CSS_LIMIT = 9076;

export async function checkOperatorBundle(buildDirectory = path.join(defaultBuildDirectory, 'operator')) {
  const page = await readFile(path.join(buildDirectory, 'operator.html'), 'utf8');
  const entries = [...page.matchAll(/(?:src|href)="\/operator\/assets\/([^"]+\.(?:js|css))"/g)].map((match) => match[1]);
  let javascript = 0;
  let css = 0;
  for (const name of entries) {
    const bytes = gzipSync(await readFile(path.join(buildDirectory, 'assets', name))).byteLength;
    if (name.endsWith('.js')) javascript += bytes;
    else css += bytes;
  }
  if (javascript > OPERATOR_JS_LIMIT) throw new Error(`Operator console JS gzip budget exceeded: ${javascript} bytes; limit is ${OPERATOR_JS_LIMIT}`);
  if (css > OPERATOR_CSS_LIMIT) throw new Error(`Operator console CSS gzip budget exceeded: ${css} bytes; limit is ${OPERATOR_CSS_LIMIT}`);
  process.stdout.write(`operator console budget passed: JS ${javascript}/${OPERATOR_JS_LIMIT} bytes, CSS ${css}/${OPERATOR_CSS_LIMIT} bytes gzip\n`);
}

const invokedPath = process.argv[1] ? pathToFileURL(path.resolve(process.argv[1])).href : '';
if (import.meta.url === invokedPath) {
  checkBundleBudget(process.argv[2]).then(() => checkOperatorBundle(process.argv[2] && path.join(process.argv[2], 'operator'))).catch((error) => {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  });
}
