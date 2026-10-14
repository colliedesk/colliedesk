// Shared facts used on every page. Change a fact here, not in page copy.
export const ORIGIN = 'https://colliedesk.com';
export const REPO = 'https://github.com/colliedesk/colliedesk';
export const NAME = 'CollieDesk';
export const UPDATED = '2026-10-14';
export const PRICES_READ = '14 October 2026';

export const TAGLINE = 'The open-source alternative to Freshdesk, Zendesk and Help Scout.';
export const POSITIONING =
  'Leave Freshdesk with all your data. AI that actually answers tickets is included, not an add-on. Flat price, no per-agent fees.';

export const FOUNDING_HREF = '/#founding';
export const PRICE = {
  monthly: '$10',
  yearly: '$100',
  founding: '$100 per year, locked for life, with free Freshdesk migration',
  ai: 'LLM cost +30%, or bring your own key',
};

// Primary navigation; order is the order shown.
export const NAV = [
  { href: '/pricing', label: 'Pricing' },
  { href: '/migrate/freshdesk', label: 'Migrate' },
  { href: '/vs/freshdesk', label: 'Compare' },
  { href: '/docs', label: 'Docs' },
  { href: '/roadmap', label: 'Roadmap' },
];

export const FOOTER = [
  {
    title: 'Product',
    links: [
      { href: '/pricing', label: 'Pricing' },
      { href: '/roadmap', label: 'Roadmap' },
      { href: '/changelog', label: 'Changelog' },
      { href: '/open-source', label: 'Open source' },
    ],
  },
  {
    title: 'Switch',
    links: [
      { href: '/migrate/freshdesk', label: 'Migrate from Freshdesk' },
      { href: '/vs/freshdesk', label: 'CollieDesk vs Freshdesk' },
      { href: '/vs/zendesk', label: 'CollieDesk vs Zendesk' },
      { href: '/vs/help-scout', label: 'CollieDesk vs Help Scout' },
    ],
  },
  {
    title: 'Build',
    links: [
      { href: '/docs', label: 'Docs' },
      { href: '/docs/install', label: 'Self-host' },
      { href: REPO, label: 'GitHub' },
      { href: '/llms.txt', label: 'llms.txt' },
    ],
  },
];
