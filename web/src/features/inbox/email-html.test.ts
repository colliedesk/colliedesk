import { expect, test } from 'vitest';
import { emailDocument } from './email-html';

const origin = 'https://desk.example.com';

function parsed(html: string): Document {
  return new DOMParser().parseFromString(emailDocument(html, origin), 'text/html');
}

test('the email document cannot run scripts, submit, navigate or load from the network', () => {
  const html = '<meta http-equiv="refresh" content="0;url=https://evil.example/r"><base href="https://evil.example/">'
    + '<link rel="stylesheet" href="https://evil.example/s.css"><script>steal()</script><p onclick="x()">Hi <a href="https://evil.example/a" target="_top">link</a></p>'
    + '<form action="https://evil.example/f"><button>go</button></form><img src="https://evil.example/p.gif" srcset="https://evil.example/2x.gif 2x">'
    + '<iframe src="https://evil.example/i"></iframe><img src="data:image/png;base64,AAAA"><div style="background:url(https://evil.example/b.png)">x</div>';
  const document = parsed(html);
  const text = emailDocument(html, origin);
  for (const forbidden of ['refresh', '<base', '<form', '<iframe', '<script', 'onclick', 'srcset', 'target="_top"', 'p.gif', '2x.gif', 's.css']) {
    expect(text).not.toContain(forbidden);
  }
  expect(document.body.textContent).toContain('Hi');
  expect(text).toContain('data:image/png;base64,AAAA');
  const policy = document.querySelector('meta[http-equiv="Content-Security-Policy"]')?.getAttribute('content') ?? '';
  for (const directive of ["default-src 'none'", "script-src 'none'", "form-action 'none'", "base-uri 'none'", "frame-src 'none'", `img-src data: ${origin}`, "style-src 'unsafe-inline'"]) {
    expect(policy).toContain(directive);
  }
  expect(policy).not.toMatch(/img-src[^;]*https?:\/\/(?!desk\.example\.com)/);
});

test('links open in a new tab with noopener and only for web and mail addresses', () => {
  const document = parsed('<a href="https://shop.example/order?id=7" target="_self">order</a> <a href="mailto:help@shop.example">mail</a> '
    + '<a href="javascript:alert(1)">js</a> <a href=" JaVaScRiPt:alert(2)">js2</a> <a href="data:text/html;base64,AAAA">data</a> <a href="#top">top</a> <a href="//evil.example/x">relative</a>');
  const links = Array.from(document.querySelectorAll('a'));
  const byText = (text: string) => links.find((link) => link.textContent === text)!;
  for (const text of ['order', 'mail']) {
    expect(byText(text).getAttribute('target')).toBe('_blank');
    expect(byText(text).getAttribute('rel')).toBe('noopener noreferrer');
  }
  expect(byText('order').getAttribute('href')).toBe('https://shop.example/order?id=7');
  expect(byText('mail').getAttribute('href')).toBe('mailto:help@shop.example');
  for (const text of ['js', 'js2', 'data', 'top', 'relative']) expect(byText(text).hasAttribute('href')).toBe(false);
});

test('images load only from the image proxy or from the message itself', () => {
  const proxied = '/api/v1/mail-image?u=https%3A%2F%2Fcdn.example%2Flogo.png&s=abc';
  const document = parsed(`<img id="a" src="${proxied.replace('&', '&amp;')}"><img id="b" src="https://cdn.example/logo.png"><img id="c" src="data:image/gif;base64,R0lGOD">`
    + '<img id="d" src="/api/v1/attachments/1"><img id="e" src="/api/v1/mail-imageX?u=1"><img id="f" src="cid:logo"><img id="g" src="//cdn.example/x.png">');
  expect(document.getElementById('a')?.getAttribute('src')).toBe(proxied);
  expect(document.getElementById('c')?.getAttribute('src')).toBe('data:image/gif;base64,R0lGOD');
  for (const id of ['b', 'd', 'e', 'f', 'g']) expect(document.getElementById(id)?.hasAttribute('src')).toBe(false);
});

const quote = '<div class="gmail_quote"><div class="gmail_attr">On Thu, Sam wrote:</div><blockquote style="border-left:1px solid #ccc">Earlier question<blockquote>Oldest question</blockquote></blockquote></div>';

test('quoted history under a reply is collapsed behind a ••• toggle that needs no script', () => {
  const document = parsed(`<div>Friday works.</div>${quote}`);
  const details = document.querySelectorAll('details.quoted-history');
  expect(details).toHaveLength(1); // the nested quote does not get a toggle of its own
  expect(details[0].hasAttribute('open')).toBe(false);
  expect(details[0].querySelector('summary')?.textContent).toBe('•••');
  expect(details[0].textContent).toContain('Oldest question');
  expect(document.body.firstElementChild?.textContent).toBe('Friday works.');
});

test('a blockquote with no gmail wrapper is collapsed too, and its attribution line stays visible', () => {
  const document = parsed('<p>Thanks, done.</p><p>On Mon, Ada wrote:</p><blockquote type="cite">Is it fixed?</blockquote>');
  expect(document.querySelectorAll('details.quoted-history')).toHaveLength(1);
  expect(document.body.textContent).toContain('On Mon, Ada wrote:');
  expect(document.querySelector('details blockquote')?.textContent).toBe('Is it fixed?');
});

test('a message that is only a quote, such as a forward, is shown open with its bar', () => {
  const document = parsed('<blockquote>Forwarded text</blockquote><p></p>');
  expect(document.querySelector('details')).toBeNull();
  expect(document.querySelector('blockquote')?.textContent).toBe('Forwarded text');
});

test('mail without quotes gets no toggle', () => {
  expect(parsed('<p>Hello</p><table><tr><td>x</td></tr></table>').querySelector('details')).toBeNull();
});

test('wide mail is made to fit and the base font is sans-serif unless the mail sets its own', () => {
  const document = parsed('<style>body{font-family:Georgia,serif}</style><table width="650"><tr><td><img width="650" src="data:image/gif;base64,R0lGOD"></td></tr></table>');
  const styles = Array.from(document.querySelectorAll('style')).map((style) => style.textContent ?? '');
  const ours = styles[0];
  expect(ours).toMatch(/font-family:[^;}]*sans-serif/);
  expect(ours).toMatch(/table\s*\{[^}]*max-width:\s*100%/);
  expect(ours).toMatch(/img\s*\{[^}]*max-width:\s*100%/);
  expect(ours).toMatch(/overflow-wrap:\s*break-word/);
  // The mail's own style comes after ours, so its font wins.
  expect(styles[1]).toContain('Georgia');
});
