import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';


// A 4 × 2 white PNG: a logo for the blue header.
const logoPNG = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAQAAAACCAIAAADwyuo0AAAADklEQVR4nGP4jwQYkDkANvEX6SAXxcIAAAAASUVORK5CYII=', 'base64');

// A long website host: on a 320 px phone it must not widen the page.
const host = 'support.international-customer-success-portal.example.co.uk';

const theme = (logo: string) => ({
  logo_media_id: logo, favicon_media_id: logo, logo_alt: 'Acme',
  linkback_url: `https://${host}/`, contact_url: 'mailto:help@acme.example', contact_phone: '+1 (555) 010-2030',
  colors: { primary: '#0b5cad', header: '#0b5cad', header_text: '#ffffff', background: '#ffffff', text: '#1f2328', link: '#0b5cad', footer: '#f6f8fa' },
  body_font: 'humanist-sans', heading_font: 'transitional-serif',
});

// noViolations runs axe on every public page in each color scheme, and expects none.
async function noViolations(page: Page, base: string, label: string, colorSchemes: readonly ('light' | 'dark')[]) {
  for (const colorScheme of colorSchemes) {
    await page.emulateMedia({ colorScheme });
    for (const path of ['', '/c/guides', '/a/1-reset-your-password', '/search?q=password']) {
      await page.goto(`${base}${path}`);
      const { violations } = await new AxeBuilder({ page }).analyze();
      expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`), `${label} ${colorScheme} ${path || '/'}`).toEqual([]);
    }
  }
}

// helpCenter signs in to a new workspace and publishes its help center with one collection and one
// article; it returns the help center's address and the headers the API wants.
async function helpCenter(page: Page, name: string) {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url, slug } = createWorkspaceWithSlug(`e2e-${name} ${suffix}`, `admin-${suffix}@example.com`);
  await page.goto(url);
  await page.getByRole('button', { name: 'Continue to Colliedesk' }).click();
  await expect(page).toHaveURL(`${servers.main}/`);
  const headers = { Origin: servers.main };
  expect((await page.request.put(`${servers.main}/api/v1/help-center`, { headers, data: { enabled: true, title: 'Acme help', locale: 'en' } })).status()).toBe(200);
  const created = await page.request.post(`${servers.main}/api/v1/kb/collections`, { headers, data: { name: 'Guides', description: 'How to use Acme' } });
  const { id: collection } = await created.json() as { id: string };
  expect((await page.request.post(`${servers.main}/api/v1/kb/articles`, { headers, data: {
    collection_id: collection, title: 'Reset your password', status: 'published',
    body_md: 'Open **Settings**, then [the security page](https://acme.example/security).\n\n> Your old password stops working at once.\n\n`Ctrl+K` opens search.',
  } })).status()).toBe(201);
  return { slug, base: `${servers.main}/help/${slug}`, headers };
}

test.describe('public pages', () => {
  // axe is a script the test injects; the help center's own CSP (script-src 'none') would refuse it.
  // Only these tests bypass it: the preview test below runs under the preview's own policy.
  test.use({ bypassCSP: true });

  test('help center branding: with the default look every page passes axe, in light and in dark', async ({ page }) => {
    const { base } = await helpCenter(page, 'default-look');
    await noViolations(page, base, 'default', ['light', 'dark']);
  });

  test('help center branding: a brand set through the API shows on every page, light only, passes axe, keeps keyboard focus and hover readable and fits a 320 px phone', async ({ page }) => {
    const { slug, base, headers } = await helpCenter(page, 'brand');
    const upload = await page.request.post(`${servers.main}/api/v1/kb/images`, { headers: { ...headers, 'Content-Type': 'image/png' }, data: logoPNG });
    expect(upload.status()).toBe(201);
    const { id: logo } = await upload.json() as { id: string };
    expect((await page.request.put(`${servers.main}/api/v1/help-center/theme`, { headers, data: theme(logo) })).status()).toBe(200);

    await page.emulateMedia({ colorScheme: 'dark' });
    await page.goto(base);
    const masthead = page.getByRole('banner');
    await expect(masthead.getByRole('link', { name: 'Acme', exact: true })).toHaveAttribute('href', `/help/${slug}`);
    const image = masthead.getByRole('img', { name: 'Acme' });
    await expect(image).toHaveJSProperty('naturalWidth', 4);
    await expect(masthead.getByRole('link', { name: host })).toHaveAttribute('href', `https://${host}/`);
    await expect(page.getByRole('contentinfo').getByRole('link', { name: 'help@acme.example' })).toHaveAttribute('href', 'mailto:help@acme.example');
    await expect(page.getByRole('contentinfo').getByRole('link', { name: '+1 (555) 010-2030' })).toHaveAttribute('href', 'tel:+15550102030');
    await expect(page.locator('link[rel="icon"]')).toHaveAttribute('href', `/help/${slug}/_/icon/${logo}`);
    // A brand is light only: a reader set to dark still sees the brand's colors.
    await expect(masthead).toHaveCSS('background-color', 'rgb(11, 92, 173)');
    await expect(page.locator('body')).toHaveCSS('background-color', 'rgb(255, 255, 255)');

    // The brand is light only (proved above under dark), so axe checks its colors once, in light.
    await noViolations(page, base, 'brand', ['light']);

    // Focus rings next to the blue header band: the skip link takes the header's white ring there, and
    // the compact search button draws its ring inside itself, in its own white ink. Hovered, the button
    // darkens (#0a5198), so its white text gains contrast.
    await page.goto(`${base}/a/1-reset-your-password`);
    await page.keyboard.press('Tab');
    const skip = page.getByRole('link', { name: 'Skip to content' });
    await expect(skip).toBeFocused();
    await expect.soft(skip).toHaveCSS('outline-color', 'rgb(255, 255, 255)');
    const searchButton = page.getByRole('banner').getByRole('button', { name: 'Search' });
    await searchButton.focus();
    await expect.soft(searchButton).toHaveCSS('outline-color', 'rgb(255, 255, 255)');
    await expect.soft(searchButton).toHaveCSS('outline-offset', '-6px');
    await searchButton.hover();
    await expect.soft(searchButton).toHaveCSS('background-color', 'rgb(10, 81, 152)');

    // On a 320 px phone the long host ends in an ellipsis instead of scrolling the page sideways; its
    // title still holds it whole.
    await page.setViewportSize({ width: 320, height: 640 });
    await page.goto(base);
    await expect.soft(masthead.getByRole('link', { name: host })).toHaveAttribute('title', host);
    const { scrollWidth, clientWidth } = await page.evaluate(() => ({
      scrollWidth: document.documentElement.scrollWidth, clientWidth: document.documentElement.clientWidth,
    }));
    expect.soft(scrollWidth, 'page width at 320 px').toBeLessThanOrEqual(clientWidth);
  });
});

// Not in the bypassCSP block: the page's own policy (style-src 'self') is enforced, so the custom CSS
// shows only because it is served from the help center's own origin, inside the brand sheet.
test('help center branding: custom CSS shows on the public page under its own policy, CSS that could load anything is refused at its line, and clearing it removes it', async ({ page }) => {
  const { base, headers } = await helpCenter(page, 'custom-css');
  const themePath = `${servers.main}/api/v1/help-center/theme`;
  const refused = await page.request.put(themePath, { headers, data: { custom_css: '.masthead{\n  border-bottom:6px solid #8b0000;\n  background:url(https://tracker.example/p.gif)\n}' } });
  expect(refused.status()).toBe(422);
  expect(await refused.json()).toMatchObject({ error: { code: 'custom_css_refused', fields: { custom_css: 'line 3, column 14: url() is not allowed' } } });

  expect((await page.request.put(themePath, { headers, data: { custom_css: '.masthead{border-bottom:6px solid #8b0000}' } })).status()).toBe(200);
  const blocked: string[] = [];
  page.on('console', (message) => { if (/Content Security Policy/i.test(message.text())) blocked.push(message.text()); });
  await page.goto(base);
  const masthead = page.getByRole('banner');
  await expect(masthead).toHaveCSS('border-bottom-width', '6px');
  await expect(masthead).toHaveCSS('border-bottom-color', 'rgb(139, 0, 0)');
  expect(blocked, 'requests the page policy refused').toEqual([]);

  expect((await page.request.put(themePath, { headers, data: { custom_css: '' } })).status()).toBe(200);
  await page.goto(base);
  await expect(masthead).not.toHaveCSS('border-bottom-width', '6px');
});

// counted wraps the person's clicks and typed inputs, so the test can hold the path to a branded help
// center to a number of steps a person would take.
function counted(page: Page) {
  const steps = { clicks: 0, typed: 0 };
  return {
    steps,
    click: async (target: ReturnType<Page['getByRole']>) => { steps.clicks++; await target.click(); },
    fill: async (target: ReturnType<Page['getByRole']>, value: string) => { steps.typed++; await target.fill(value); },
    upload: async (button: ReturnType<Page['getByRole']>, file: { name: string; mimeType: string; buffer: Buffer }) => {
      const chooser = page.waitForEvent('filechooser');
      steps.clicks++;
      await button.click();
      steps.typed++;
      await (await chooser).setFiles(file);
    },
  };
}

test('help center branding: from Settings to a branded public page takes at most 6 clicks and 2 typed inputs, with a live preview that runs nothing', async ({ page, context }) => {
  const { slug } = await helpCenter(page, 'time-to-branded');
  const person = counted(page);
  const started = Date.now();

  await person.click(page.getByRole('link', { name: 'Settings' }));
  const brand = page.getByRole('region', { name: 'Brand' });
  await person.upload(brand.getByRole('button', { name: 'Upload logo' }), { name: 'acme-logo.png', mimeType: 'image/png', buffer: logoPNG });
  await expect(brand.getByRole('img', { name: 'Logo' })).toBeVisible();
  await person.fill(brand.getByRole('textbox', { name: 'Primary color', exact: true }), '#0b5cad');

  // The preview is the real page with the candidate brand: the logo loads from the library with the
  // session, inside a frame that runs no script and follows no link.
  const frame = brand.locator('iframe[title="Preview of the help center"]');
  await expect(frame).toHaveAttribute('sandbox', 'allow-same-origin');
  await expect(frame).toHaveAttribute('srcdoc', /--accent:#0b5cad;/);
  await expect(frame).toHaveAttribute('srcdoc', /script-src 'none'/);
  const inside = page.frameLocator('iframe[title="Preview of the help center"]');
  // Under the preview's own policy (CSP is not bypassed here): the logo loads from the library, the help
  // center's sheet from its hashed address and the brand from the inline block.
  await expect(inside.getByRole('banner').getByRole('img', { name: 'Acme help' })).toHaveJSProperty('naturalWidth', 4);
  await expect(inside.getByRole('button', { name: 'Search', exact: true })).toHaveCSS('background-color', 'rgb(11, 92, 173)');
  // Not a step to the branded page, so not counted: a link in the preview leaves the page where it is,
  // clicked or opened from the keyboard. A navigation would be a framenavigated event away from
  // about:srcdoc; the race gives it 1.5 s after both attempts and expects none.
  const preview = await (await frame.elementHandle())!.contentFrame();
  expect(preview).not.toBeNull();
  await preview!.waitForLoadState('load');
  const navigated = page.waitForEvent('framenavigated', { predicate: (changed) => changed === preview && changed.url() !== 'about:srcdoc', timeout: 1_500 })
    .then((changed) => changed.url(), () => undefined);
  const guides = inside.getByRole('link', { name: 'Guides' }).first();
  // force: the link takes no pointer (that is the point), so the click lands where a person's would.
  await guides.click({ force: true });
  await guides.focus();
  await page.keyboard.press('Enter');
  expect(await navigated, 'address the preview navigated to').toBeUndefined();
  expect(preview!.url()).toBe('about:srcdoc');
  await expect(inside.getByRole('heading', { name: 'How can we help?' })).toBeVisible();

  await person.click(brand.getByRole('button', { name: 'Save brand' }));
  await expect(brand.getByText('Saved. Live on the next page load.')).toBeVisible();
  const opened = context.waitForEvent('page');
  await person.click(page.getByRole('link', { name: `${servers.main}/help/${slug}` }));
  const site = await opened;
  const masthead = site.getByRole('banner');
  await expect(masthead.getByRole('img', { name: 'Acme help' })).toHaveJSProperty('naturalWidth', 4);
  await expect(site.getByRole('button', { name: 'Search', exact: true })).toHaveCSS('background-color', 'rgb(11, 92, 173)');

  const elapsed = Date.now() - started;
  console.log(`time-to-branded: ${person.steps.clicks} clicks, ${person.steps.typed} typed inputs, ${elapsed} ms`);
  expect(person.steps.clicks, 'clicks from Settings to the branded page').toBeLessThanOrEqual(6);
  expect(person.steps.typed, 'typed inputs from Settings to the branded page').toBeLessThanOrEqual(2);
});
