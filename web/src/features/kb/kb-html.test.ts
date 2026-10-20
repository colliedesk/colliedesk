import { describe, expect, it } from 'vitest';
import allowlist from '../../../../server/internal/kbhtml/allowlist.json';
import shared from '../../../../server/internal/kbhtml/testdata/cases.json';
import { fragment } from './kb-html';


// The cases the help center runs too (server/internal/kbhtml/kbhtml_test.go).
describe('raw HTML in the preview follows the help center', () => {
  it.each(shared.cases.map((c) => [c.name, c] as const))('%s', (_name, c) => {
    const { nodes, dropped } = fragment(c.html);
    expect(nodes.map((node) => node.properties.src)).toEqual(c.iframes);
    expect(dropped).toEqual(c.dropped);
    if ('titles' in c) expect(nodes.map((node) => node.properties.title)).toEqual(c.titles);
    for (const node of nodes) {
      expect(node.tagName).toBe('iframe');
      expect(node.properties).toMatchObject({
        sandbox: 'allow-scripts allow-same-origin allow-presentation allow-popups',
        referrerPolicy: 'strict-origin-when-cross-origin',
        loading: 'lazy',
      });
    }
  });
});

it('rebuilds a player with only its allowed attributes', () => {
  const { nodes, dropped } = fragment(
    `<IFRAME SRC="https://www.youtube.com/embed/jNQXAC9IVRw?start=65" title='Me &amp; "you"' width="560" height="315" allow="fullscreen; encrypted-media; camera" allowfullscreen></IFRAME>`,
  );
  expect(nodes).toEqual([
    {
      type: 'element',
      tagName: 'iframe',
      properties: {
        src: 'https://www.youtube-nocookie.com/embed/jNQXAC9IVRw?start=65',
        title: 'Me & "you"',
        width: '560',
        height: '315',
        allow: 'encrypted-media',
        allowFullScreen: true,
        sandbox: 'allow-scripts allow-same-origin allow-presentation allow-popups',
        referrerPolicy: 'strict-origin-when-cross-origin',
        loading: 'lazy',
      },
      children: [],
    },
  ]);
  expect(dropped).toEqual(['iframe allow camera']);
});

// The <video> rule is ready for the media library and off until it ships.
it('keeps a media library video once the library is on', () => {
  const rules = { ...allowlist, video: { ...allowlist.video, enabled: true } };
  const id = '/api/v1/kb/media/0b6f8a8e-3c4d-4e5f-8a9b-0c1d2e3f4a5b';
  const { nodes, dropped } = fragment(
    `<video src="${id}" poster="${id}/poster" title="Tour" aria-label="Product tour" width="640" preload="auto" autoplay muted playsinline onplay="x()"><source src="https://evil.example/a.mp4"><track src="x"></video>`,
    rules,
  );
  expect(nodes).toEqual([
    {
      type: 'element',
      tagName: 'video',
      properties: { src: id, title: 'Tour', ariaLabel: 'Product tour', width: '640', preload: 'metadata', muted: true, playsInline: true, controls: true },
      children: [],
    },
  ]);
  expect(dropped).toEqual(['video preload=auto', 'video poster', 'video autoplay', 'video onplay', 'source', 'track']);
  for (const bad of [`${id}X`, 'https://evil.example/a.mp4', id.toUpperCase(), `${id}?x=1`])
    expect(fragment(`<video src="${bad}"></video>`, rules).nodes).toEqual([]);
  expect(fragment(`<video src="${id}"></video>`).nodes).toEqual([]);
});
