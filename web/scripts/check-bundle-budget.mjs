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

const invokedPath = process.argv[1] ? pathToFileURL(path.resolve(process.argv[1])).href : '';
if (import.meta.url === invokedPath) {
  checkBundleBudget(process.argv[2]).catch((error) => {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  });
}
