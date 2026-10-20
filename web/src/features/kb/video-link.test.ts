import { describe, expect, it } from 'vitest';
import { fragment } from './kb-html';
import { embedSnippet, parseVideoLink } from './video-link';

describe('a pasted video link becomes a player address', () => {
  it.each([
    ['https://www.youtube.com/watch?v=jNQXAC9IVRw', 'https://www.youtube-nocookie.com/embed/jNQXAC9IVRw'],
    ['https://youtube.com/watch?v=jNQXAC9IVRw&list=PL1&t=1m5s', 'https://www.youtube-nocookie.com/embed/jNQXAC9IVRw?start=65'],
    ['https://youtu.be/jNQXAC9IVRw?t=90', 'https://www.youtube-nocookie.com/embed/jNQXAC9IVRw?start=90'],
    ['https://youtu.be/jNQXAC9IVRw?t=1h2m3s', 'https://www.youtube-nocookie.com/embed/jNQXAC9IVRw?start=3723'],
    ['https://m.youtube.com/shorts/jNQXAC9IVRw', 'https://www.youtube-nocookie.com/embed/jNQXAC9IVRw'],
    ['https://www.youtube.com/embed/jNQXAC9IVRw?start=12', 'https://www.youtube-nocookie.com/embed/jNQXAC9IVRw?start=12'],
    ['  https://vimeo.com/76979871  ', 'https://player.vimeo.com/video/76979871'],
    ['https://vimeo.com/76979871/8272103f6e', 'https://player.vimeo.com/video/76979871?h=8272103f6e'],
    ['https://player.vimeo.com/video/76979871', 'https://player.vimeo.com/video/76979871'],
  ])('%s', (link, src) => {
    expect(parseVideoLink(link)).toEqual({ src, title: src.includes('vimeo') ? 'Vimeo video' : 'YouTube video' });
  });

  it.each([
    ['youtube.com/watch?v=jNQXAC9IVRw', 'Paste the whole link, starting with https://.'],
    ['http://youtu.be/jNQXAC9IVRw', 'Paste the whole link, starting with https://.'],
    ['https://youtu.be:8443/jNQXAC9IVRw', 'Paste the whole link, starting with https://.'],
    ['https://evil.example/watch?v=jNQXAC9IVRw', 'evil.example is not a video site articles can show: paste a YouTube or Vimeo link.'],
    ['https://cdn.example.com/tour.mp4', 'A link to a video file cannot be shown: paste a YouTube or Vimeo link.'],
    ['https://www.youtube.com/watch?v=short', 'This YouTube or Vimeo link names no video: copy the link from the video\'s Share button.'],
  ])('%s is refused and says why', (link, error) => {
    expect(parseVideoLink(link)).toEqual({ error });
  });
});

it('writes a player the help center keeps as it is', () => {
  expect(embedSnippet({ src: 'https://player.vimeo.com/video/1?h=ab12cd', title: 'Vimeo video' })).toBe(
    '<iframe src="https://player.vimeo.com/video/1?h=ab12cd" title="Vimeo video" width="560" height="315" allow="encrypted-media; picture-in-picture" allowfullscreen loading="lazy"></iframe>',
  );
  expect(fragment(embedSnippet({ src: 'https://www.youtube-nocookie.com/embed/jNQXAC9IVRw?start=65', title: 'YouTube video' })).dropped).toEqual([]);
});
