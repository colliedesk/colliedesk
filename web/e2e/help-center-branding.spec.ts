import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

// axe is a script the test injects; the help center's own CSP (script-src 'none') would refuse it.
test.use({ bypassCSP: true });

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
