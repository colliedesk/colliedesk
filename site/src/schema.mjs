// JSON-LD builders. Only facts from site.mjs; no ratings, reviews or counts we do not have.
import { ORIGIN, REPO, NAME, POSITIONING, UPDATED } from './site.mjs';

const plain = (md) => md.replace(/\[([^\]]+)\]\([^)]+\)/g, '$1').replace(/[*_`]/g, '');

export const organization = () => ({
  '@context': 'https://schema.org',
  '@type': 'Organization',
  name: 'HC Labs',
  url: ORIGIN,
  sameAs: [REPO],
});

export const softwareApplication = () => ({
  '@context': 'https://schema.org',
  '@type': 'SoftwareApplication',
  name: NAME,
  url: ORIGIN,
  description: `Open-source helpdesk for small support teams. ${POSITIONING}`,
  applicationCategory: 'BusinessApplication',
  applicationSubCategory: 'Help desk software',
  operatingSystem: 'Linux, Docker; web browser',
  softwareVersion: '0.1 (pre-alpha)',
  license: 'https://www.gnu.org/licenses/agpl-3.0.html',
  codeRepository: REPO,
  isAccessibleForFree: true,
  author: { '@type': 'Organization', name: 'HC Labs', url: ORIGIN },
  dateModified: UPDATED,
  offers: [
    { '@type': 'Offer', name: 'Self-hosted', price: '0', priceCurrency: 'USD', description: 'Free forever, every feature, AGPL-3.0.' },
    {
      '@type': 'Offer', name: 'CollieDesk Cloud', price: '10', priceCurrency: 'USD',
      description: 'Per workspace per month ($100 per year). No per-agent fees.',
      priceSpecification: { '@type': 'UnitPriceSpecification', price: '10', priceCurrency: 'USD', unitText: 'workspace per month' },
    },
    {
      '@type': 'Offer', name: 'Founding customer', price: '100', priceCurrency: 'USD',
      description: 'Per workspace per year, locked for life, with free Freshdesk migration.',
      priceSpecification: { '@type': 'UnitPriceSpecification', price: '100', priceCurrency: 'USD', unitText: 'workspace per year' },
    },
  ],
});

export const faqPage = (faq) => ({
  '@context': 'https://schema.org',
  '@type': 'FAQPage',
  mainEntity: faq.map((f) => ({
    '@type': 'Question',
    name: f.q,
    acceptedAnswer: { '@type': 'Answer', text: plain(f.a) },
  })),
});

export const techArticle = (page) => ({
  '@context': 'https://schema.org',
  '@type': 'TechArticle',
  headline: page.h1,
  description: page.description,
  url: `${ORIGIN}${page.path}`,
  dateModified: page.updated || UPDATED,
  author: { '@type': 'Organization', name: 'HC Labs' },
  about: { '@type': 'SoftwareApplication', name: NAME },
});
