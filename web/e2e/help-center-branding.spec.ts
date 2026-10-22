import AxeBuilder from '@axe-core/playwright';
import { expect, test } from '@playwright/test';
import type { Page } from '@playwright/test';
import { servers } from './support/cli';
import { createWorkspaceWithSlug } from './support/workspace';

// axe is a script the test injects; the help center's own CSP (script-src 'none') would refuse it.
test.use({ bypassCSP: true });

// A 4 × 2 white PNG: a logo for the blue header.
const logoPNG = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAQAAAACCAIAAADwyuo0AAAADklEQVR4nGP4jwQYkDkANvEX6SAXxcIAAAAASUVORK5CYII=', 'base64');

const theme = (logo: string) => ({
  logo_media_id: logo, favicon_media_id: logo, logo_alt: 'Acme',
  linkback_url: 'https://acme.example/', contact_url: 'mailto:help@acme.example', contact_phone: '+1 (555) 010-2030',
  colors: { primary: '#0b5cad', header: '#0b5cad', header_text: '#ffffff', background: '#ffffff', text: '#1f2328', link: '#0b5cad', footer: '#f6f8fa' },
  body_font: 'humanist-sans', heading_font: 'transitional-serif',
});

// noViolations runs axe on every public page in light and dark, and expects none.
async function noViolations(page: Page, base: string, label: string) {
  for (const colorScheme of ['light', 'dark'] as const) {
    await page.emulateMedia({ colorScheme });
    for (const path of ['', '/c/guides', '/a/1-reset-your-password', '/search?q=password']) {
      await page.goto(`${base}${path}`);
      const { violations } = await new AxeBuilder({ page }).analyze();
      expect(violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(' ')).join(', ')}`), `${label} ${colorScheme} ${path || '/'}`).toEqual([]);
    }
  }
}

test('help center branding: a brand set through the API shows on every page, light only, and the pages pass axe with the default look and with the brand', async ({ page }) => {
  const suffix = `${Date.now()}-${process.pid}`;
  const { url, slug } = createWorkspaceWithSlug(`e2e-brand ${suffix}`, `admin-${suffix}@example.com`);
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
  const base = `${servers.main}/help/${slug}`;

  await noViolations(page, base, 'default');

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
  await expect(masthead.getByRole('link', { name: 'acme.example' })).toHaveAttribute('href', 'https://acme.example/');
  await expect(page.getByRole('contentinfo').getByRole('link', { name: 'help@acme.example' })).toHaveAttribute('href', 'mailto:help@acme.example');
  await expect(page.getByRole('contentinfo').getByRole('link', { name: '+1 (555) 010-2030' })).toHaveAttribute('href', 'tel:+15550102030');
  await expect(page.locator('link[rel="icon"]')).toHaveAttribute('href', `/help/${slug}/_/icon/${logo}`);
  // A brand is light only: a reader set to dark still sees the brand's colors.
  await expect(masthead).toHaveCSS('background-color', 'rgb(11, 92, 173)');
  await expect(page.locator('body')).toHaveCSS('background-color', 'rgb(255, 255, 255)');

  await noViolations(page, base, 'brand');
});
