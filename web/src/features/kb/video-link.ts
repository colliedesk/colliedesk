import { embedSrc } from './kb-html';
import { kbMessages } from './kb-messages';

// Reads what an author pastes to embed a video (a watch page, a short link, a Vimeo page or a player address)
// and gives the player address the help center shows, or says why the link cannot be used.

export interface VideoEmbed {
  src: string;
  title: string;
}

// seconds reads YouTube's t= (90, 90s, 1m30s, 1h2m3s).
function seconds(value: string | null) {
  const match = value ? /^(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s?)?$/.exec(value) : null;
  if (!match?.[0]) return undefined;
  const total = Number(match[1] ?? 0) * 3600 + Number(match[2] ?? 0) * 60 + Number(match[3] ?? 0);
  return total > 0 ? String(total) : undefined;
}

export function parseVideoLink(raw: string): VideoEmbed | { error: string } {
  const value = raw.trim();
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return { error: kbMessages.videoLinkNotHttps };
  }
  if (url.protocol !== 'https:' || url.username || url.password || url.port) return { error: kbMessages.videoLinkNotHttps };
  const host = url.hostname.replace(/^(www|m)\./, '');
  const start = seconds(url.searchParams.get('t')) ?? url.searchParams.get('start') ?? undefined;
  const youtube = (id: string | null | undefined) =>
    `https://www.youtube-nocookie.com/embed/${id ?? ''}${start ? `?start=${start}` : ''}`;
  let candidate = value;
  if (host === 'youtube.com' && url.pathname === '/watch') candidate = youtube(url.searchParams.get('v'));
  else if (host === 'youtube.com' || host === 'youtube-nocookie.com')
    candidate = youtube(/^\/(?:shorts|live|embed)\/([^/]+)/.exec(url.pathname)?.[1]);
  else if (host === 'youtu.be') candidate = youtube(url.pathname.slice(1));
  else if (host === 'vimeo.com') {
    const match = /^\/(\d+)(?:\/([0-9a-f]+))?\/?$/.exec(url.pathname);
    if (match) candidate = `https://player.vimeo.com/video/${match[1]}${match[2] ? `?h=${match[2]}` : ''}`;
  }
  const embed = embedSrc(candidate);
  if (embed?.src) return { src: embed.src, title: embed.site.title };
  if (['youtube.com', 'youtube-nocookie.com', 'youtu.be', 'vimeo.com', 'player.vimeo.com'].includes(host))
    return { error: kbMessages.videoLinkNoVideo };
  if (/\.(mp4|webm|mov|m4v|ogv)$/i.test(url.pathname)) return { error: kbMessages.videoLinkFile };
  return { error: kbMessages.videoLinkOtherSite(url.host) };
}

const attribute = (value: string) => value.replace(/&/g, '&amp;').replace(/"/g, '&quot;');

// embedSnippet is the HTML put in the article: kb-html.ts and the help center keep every attribute of it.
export const embedSnippet = ({ src, title }: VideoEmbed) =>
  `<iframe src="${attribute(src)}" title="${attribute(title)}" width="560" height="315" allow="encrypted-media; picture-in-picture" allowfullscreen loading="lazy"></iframe>`;
