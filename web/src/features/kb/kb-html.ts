import allowlist from '../../../../server/internal/kbhtml/allowlist.json';

// Raw HTML in an article, as the preview shows it: the same rules as the help center
// (server/internal/kbhtml, which reads the same allowlist.json and runs the same testdata/cases.json). A
// YouTube or Vimeo player is rebuilt from its video id; everything else in the HTML is left out, its text
// too (the words between inline tags are Markdown and stay).
// The HTML is parsed by DOMParser, whose document runs no script and loads nothing. The saved Markdown keeps
// every byte; only what is shown leaves HTML out.

export type Rules = typeof allowlist;
type Embed = Rules['iframe']['embeds'][number];

export interface HtmlElement {
  type: 'element';
  tagName: 'iframe' | 'video';
  properties: Record<string, string | boolean>;
  children: [];
}
export type HtmlNode = HtmlElement;

// A space, control character or backslash inside an address is refused: browsers read those in ways the
// server's parser does not.
const unsafeAddress = /[\u0000- \u007f\\]/;
const trim = (raw: string) => raw.replace(/^[\u0000- ]+|[\u0000- ]+$/g, '');

// hostOf names where a refused address points, for the warning.
export function hostOf(raw: string) {
  const value = trim(raw);
  if (!value) return 'no src';
  if (!unsafeAddress.test(value)) {
    try {
      const url = new URL(value);
      return url.host ? url.host.toLowerCase() : url.protocol.toLowerCase();
    } catch {
      // A relative address: named as written.
    }
  }
  return Array.from(value).slice(0, 40).join('');
}

// embedSrc is the player address an iframe may load, built again from the video id in raw, so no path,
// query or host of the author's comes through; undefined when raw is no player of the list.
export function embedSrc(raw: string, rules: Rules = allowlist): { site: Embed; src: string } | undefined {
  const value = trim(raw);
  if (unsafeAddress.test(value)) return undefined;
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return undefined;
  }
  if (url.protocol !== 'https:' || url.username || url.password || url.port || !url.host) return undefined;
  // The path as written, not as the URL parser resolved it (dot segments), as on the server.
  const prefix = `https://${url.host}`;
  const rest = value.slice(prefix.length);
  if (!value.toLowerCase().startsWith(prefix) || !rest.startsWith(url.pathname) || !/^($|[?#])/.test(rest.slice(url.pathname.length)))
    return undefined;
  const site = rules.iframe.embeds.find((embed) => embed.hosts.includes(url.hostname));
  if (!site) return undefined;
  const id = new RegExp(site.path).exec(url.pathname)?.[1];
  if (!id) return undefined;
  const query = new URLSearchParams();
  for (const [key, pattern] of Object.entries(site.query)) {
    const given = url.searchParams.get(key);
    if (pattern && given && new RegExp(pattern).test(given)) query.set(key, given);
  }
  const search = query.toString();
  return { site, src: `${site.src}${id}${search ? `?${search}` : ''}` };
}

const XHTML = 'http://www.w3.org/1999/xhtml';

class Fragment {
  readonly nodes: HtmlNode[] = [];
  readonly dropped: string[] = [];
  constructor(private readonly rules: Rules) {}

  drop(what: string) {
    if (!this.dropped.includes(what)) this.dropped.push(what);
  }

  dimension(value: string) {
    const { pattern, min, max } = this.rules.dimension;
    return new RegExp(pattern).test(value) && Number(value) >= min && Number(value) <= max;
  }

  walk(node: Node) {
    if (node.nodeType !== Node.ELEMENT_NODE) return;
    const element = node as Element;
    const name = element.localName;
    if (element.namespaceURI === XHTML && name === 'iframe') return this.iframe(element);
    if (element.namespaceURI === XHTML && name === 'video') return this.video(element);
    this.drop(name);
    if (this.rules.dropWithContent.includes(name)) return;
    // Any other element is left out with its text, as raw HTML always was; a player inside it is kept.
    for (const child of Array.from(element.childNodes)) this.walk(child);
  }

  iframe(element: Element) {
    const { iframe } = this.rules;
    const src = element.getAttribute('src');
    const embed = src === null ? undefined : embedSrc(src, this.rules);
    if (!embed) {
      this.drop(src === null ? 'iframe (no src)' : `iframe (${hostOf(src)})`);
      return;
    }
    const properties: HtmlElement['properties'] = { src: embed.src };
    const title = element.getAttribute('title');
    properties.title = title?.trim() ? title : embed.site.title;
    for (const key of ['width', 'height']) {
      const value = element.getAttribute(key);
      if (value === null) continue;
      if (this.dimension(value)) properties[key] = value;
      else this.drop(`iframe ${key}`);
    }
    const fullscreen = element.hasAttribute('allowfullscreen');
    const allow = element.getAttribute('allow');
    if (allow !== null) {
      const kept: string[] = [];
      for (const token of allow.split(';')) {
        const feature = token.trim().split(/\s+/)[0]?.toLowerCase();
        if (!feature) continue;
        if (!iframe.allow.includes(feature)) this.drop(`iframe allow ${feature}`);
        // allowfullscreen and allow="fullscreen" say the same; browsers warn when both are there.
        else if (!(feature === 'fullscreen' && fullscreen) && !kept.includes(feature)) kept.push(feature);
      }
      if (kept.length) properties.allow = kept.join('; ');
    }
    if (fullscreen) properties.allowFullScreen = true;
    for (const { name, value } of Array.from(element.attributes))
      if (!iframe.attributes.includes(name) || (name === 'loading' && value !== iframe.forced.loading))
        this.drop(`iframe ${name}`);
    properties.sandbox = iframe.forced.sandbox;
    properties.referrerPolicy = iframe.forced.referrerpolicy;
    properties.loading = iframe.forced.loading;
    this.nodes.push({ type: 'element', tagName: 'iframe', properties, children: [] });
  }

  video(element: Element) {
    const { video } = this.rules;
    const src = element.getAttribute('src');
    if (src === null) return this.drop('video (no src)');
    if (!new RegExp(video.src).test(src)) return this.drop(`video (${hostOf(src)})`);
    if (!video.enabled) return this.drop('video (media library)');
    const properties: HtmlElement['properties'] = { src };
    // The poster, and a missing width or height, will come from the media library, not the article.
    for (const key of ['title', 'aria-label']) {
      const value = element.getAttribute(key);
      if (value !== null) properties[key === 'aria-label' ? 'ariaLabel' : key] = value;
    }
    for (const key of ['width', 'height']) {
      const value = element.getAttribute(key);
      if (value === null) continue;
      if (this.dimension(value)) properties[key] = value;
      else this.drop(`video ${key}`);
    }
    const preload = element.getAttribute('preload');
    if (preload !== null && !video.preload.includes(preload)) this.drop(`video preload=${preload}`);
    properties.preload = preload !== null && video.preload.includes(preload) ? preload : 'metadata';
    const flag: Record<string, string> = { playsinline: 'playsInline' };
    for (const name of video.flags) if (element.hasAttribute(name)) properties[flag[name] ?? name] = true;
    for (const { name } of Array.from(element.attributes)) if (!video.attributes.includes(name)) this.drop(`video ${name}`);
    for (const child of Array.from(element.children)) this.drop(child.localName);
    // Controls are not the author's choice: a video always has them.
    properties.controls = true;
    this.nodes.push({ type: 'element', tagName: 'video', properties, children: [] });
  }
}

// fragment turns one piece of raw HTML, as the Markdown parser hands it over, into the players the preview
// may show, and names the elements it left out, in order and once each. Text, a closing tag alone or a
// comment shows nothing and is not named.
export function fragment(raw: string, rules: Rules = allowlist): { nodes: HtmlNode[]; dropped: string[] } {
  const known = rules === allowlist ? seen.get(raw) : undefined;
  const result = known ?? parse(raw, rules);
  if (rules === allowlist && !known) {
    // Closing tags and repeated players come back on every keystroke; parse each piece once.
    if (seen.size >= 2000) seen.clear();
    seen.set(raw, result);
  }
  // Copies: the Markdown renderer writes to the nodes it is given.
  return {
    nodes: result.nodes.map((node) => ({ ...node, properties: { ...node.properties } })),
    dropped: [...result.dropped],
  };
}

const seen = new Map<string, { nodes: HtmlNode[]; dropped: string[] }>();
let parser: DOMParser | undefined;

function parse(raw: string, rules: Rules) {
  parser ??= new DOMParser();
  const document = parser.parseFromString(raw, 'text/html');
  const result = new Fragment(rules);
  // A fragment that starts with <style>, <script> or <title> puts it in the head.
  for (const node of [...Array.from(document.head.childNodes), ...Array.from(document.body.childNodes)]) {
    // Words on the lines of an HTML block are part of it, so they go too; say so.
    if (node.nodeType === Node.TEXT_NODE && /[^\t\n\f\r ]/.test(node.nodeValue ?? '')) result.drop('text in an HTML block');
    result.walk(node);
  }
  return { nodes: result.nodes, dropped: result.dropped };
}
