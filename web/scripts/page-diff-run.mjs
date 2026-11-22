// Screenshots pairs of pages (an old help center and its Colliedesk copy) and scores how far apart they are.
//
//   node scripts/page-diff-run.mjs --config pairs.json --out /tmp/page-diff [--calibrate]
//
// pairs.json: { "old": "https://support.example.com", "new": "https://example.colliedesk.com",
//               "pages": { "home": ["/support/home", "/help"] }, "widths": [1280, 390],
//               "blocks": { "old": { "header": "header.banner" }, "new": { "header": "header.masthead" } } }
// "widths" and "blocks" are optional. A block may list several selectors: the first one with a visible match
// on the page is used, so the content column can be the article on one page and the topic list on another. --calibrate renders each old page twice and also shifts it by 1px,
// which gives the noise floor and the cost of a 1px offset to set a threshold against.
// Screenshots stay in --out: they show a customer's site, so never commit them.
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { parseArgs } from 'node:util';
import { chromium } from '@playwright/test';
import { diffFraction, layoutScore, shiftDown } from './page-diff.mjs';

const DEFAULT_BLOCKS = {
  // Freshdesk's classic portal theme.
  old: { header: 'header.banner', search: '.hc-search', content: 'section.main', sidebar: 'section.sidebar', footer: 'footer.footer' },
  // Colliedesk's help center templates.
  new: {
    header: 'header.masthead',
    search: ['form.search-hero', 'form.search-compact'],
    content: ['article.article', 'section.topics', 'main#main'],
    sidebar: 'aside.rail',
    footer: 'footer.foot',
  },
};

const { values } = parseArgs({ options: { config: { type: 'string' }, out: { type: 'string' }, calibrate: { type: 'boolean' } } });
if (!values.config || !values.out) {
  console.error('usage: node scripts/page-diff-run.mjs --config pairs.json --out DIR [--calibrate]');
  process.exit(2);
}
const config = JSON.parse(readFileSync(values.config, 'utf8'));
const blocks = { old: config.blocks?.old ?? DEFAULT_BLOCKS.old, new: config.blocks?.new ?? DEFAULT_BLOCKS.new };
mkdirSync(values.out, { recursive: true });

const browser = await chromium.launch({ executablePath: existsSync('/usr/bin/google-chrome') ? '/usr/bin/google-chrome' : undefined });
const decoder = await browser.newPage();

// Renders url and returns the page image plus the box of the first visible match of each block selector.
async function render(page, url, selectors, file) {
  await page.goto(url, { waitUntil: 'networkidle', timeout: 45000 });
  await page.evaluate(() => document.fonts.ready);
  const boxes = await page.evaluate((selectors) => {
    const out = {};
    for (const [name, choice] of Object.entries(selectors)) {
      let el;
      for (const selector of [choice].flat()) {
        el = [...document.querySelectorAll(selector)].find((e) => e.getClientRects().length > 0);
        if (el) break;
      }
      if (!el) continue;
      const r = el.getBoundingClientRect();
      if (r.width > 0 && r.height > 0) out[name] = { x: r.x + scrollX, y: r.y + scrollY, width: r.width, height: r.height };
    }
    return out;
  }, selectors);
  const png = await page.screenshot({ fullPage: true, animations: 'disabled', caret: 'hide' });
  writeFileSync(file, png);
  return { image: await decode(png), boxes };
}

// Decodes a PNG with the browser's canvas, the one PNG decoder this package already has.
async function decode(png) {
  const { width, height, rgba } = await decoder.evaluate(async (b64) => {
    const bitmap = await createImageBitmap(await (await fetch(`data:image/png;base64,${b64}`)).blob());
    const height = bitmap.height;
    const canvas = new OffscreenCanvas(bitmap.width, height);
    const ctx = canvas.getContext('2d');
    ctx.drawImage(bitmap, 0, 0);
    const bytes = ctx.getImageData(0, 0, bitmap.width, height).data;
    const url = await new Promise((resolve) => {
      const reader = new FileReader();
      reader.onload = () => resolve(reader.result);
      reader.readAsDataURL(new Blob([bytes]));
    });
    return { width: bitmap.width, height, rgba: url.slice(url.indexOf(',') + 1) };
  }, png.toString('base64'));
  return { width, height, data: new Uint8ClampedArray(Buffer.from(rgba, 'base64')) };
}

const pct = (f) => `${(f * 100).toFixed(1)}%`;
const rows = [];
for (const width of config.widths ?? [1280, 390]) {
  const context = await browser.newContext({ viewport: { width, height: 900 }, deviceScaleFactor: 1 });
  const page = await context.newPage();
  for (const [name, [oldPath, newPath]] of Object.entries(config.pages)) {
    const tag = `${name}-${width}`;
    const old = await render(page, config.old + oldPath, blocks.old, join(values.out, `${tag}-old.png`));
    const row = { page: name, width };
    if (values.calibrate) {
      const again = await render(page, config.old + oldPath, blocks.old, join(values.out, `${tag}-old-again.png`));
      row.noisePixel = diffFraction(old.image, again.image).fraction;
      const noise = layoutScore(old.image, again.image, old.boxes, again.boxes);
      row.noiseBlockPixel = noise.fraction;
      row.noiseGeometry = noise.geometry;
      row.shift1px = diffFraction(old.image, shiftDown(old.image, 1)).fraction;
    }
    const copy = await render(page, config.new + newPath, blocks.new, join(values.out, `${tag}-new.png`));
    const layout = layoutScore(old.image, copy.image, old.boxes, copy.boxes);
    Object.assign(row, {
      pixel: diffFraction(old.image, copy.image).fraction,
      geometry: layout.geometry,
      blockPixel: layout.fraction,
      missing: layout.missing,
      uncaptured: layout.uncaptured,
      extra: layout.extra,
      blocks: layout.blocks,
    });
    rows.push(row);
    console.error(`scored ${tag}`);
  }
  await context.close();
}
await browser.close();

writeFileSync(join(values.out, 'scores.json'), `${JSON.stringify(rows, null, 2)}\n`);
const head = ['page', 'width', 'geometry diff', 'in-block pixel diff', 'whole-page pixel diff', 'missing', 'not captured', 'only on new'];
if (values.calibrate) head.push('noise geometry', 'noise in-block', 'noise page', '1px shift');
const lines = [`| ${head.join(' | ')} |`, `|${head.map(() => '---').join('|')}|`];
for (const r of rows) {
  const list = (names) => names.join(', ') || '–';
  const cells = [r.page, r.width, pct(r.geometry), pct(r.blockPixel), pct(r.pixel), list(r.missing), list(r.uncaptured), list(r.extra)];
  if (values.calibrate) cells.push(pct(r.noiseGeometry), pct(r.noiseBlockPixel), pct(r.noisePixel), pct(r.shift1px));
  lines.push(`| ${cells.join(' | ')} |`);
}
console.log(lines.join('\n'));
