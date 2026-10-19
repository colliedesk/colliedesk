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

// The frame shares the app's origin and its cookie, so a CSP source of the whole origin would let mail CSS
// (url() in a style) make the browser send authenticated GETs to any API route, such as /api/v1/export.
test('the policy is exactly this: images only from data: and the image proxy path, nothing else fetchable', () => {
  const policy = parsed('<p>x</p>').querySelector('meta[http-equiv="Content-Security-Policy"]')?.getAttribute('content');
  expect(policy).toBe("default-src 'none'; script-src 'none'; style-src 'unsafe-inline'; img-src data: https://desk.example.com/api/v1/mail-image; form-action 'none'; base-uri 'none'; frame-src 'none'");
  expect(policy).not.toMatch(/(font|media|connect|object|child|worker|manifest)-src\s+[^;']*\S/);
});

test('mail cannot size itself by the frame viewport, which would make the frame chase its own height', () => {
  const html = '<style>html,body{height:100%;min-height:100vh}.hero{height:100vh;min-height:50svh}.keep{max-height:100vh;width:100vw;line-height:1.2}</style>'
    + '<div class="hero" style="min-height:100vh;height: 80.5vh;padding:5px">a</div><div style="height:120px">b</div>';
  const text = emailDocument(html, origin);
  expect(text).not.toMatch(/(?:^|[;{\s])(?:min-)?height\s*:\s*[\d.]+(?:vh|svh|dvh|lvh)/i);
  expect(text).toContain('max-height:100vh'); // a ceiling cannot grow the frame
  expect(text).toContain('height:120px');
  expect(text).toContain('padding:5px');
  const document = parsed(html);
  const last = Array.from(document.querySelectorAll('style')).at(-1)!.textContent ?? '';
  expect(last).toContain('html,body{height:auto!important;min-height:0!important}');
  expect(document.body.lastElementChild?.tagName).toBe('STYLE'); // after the mail's own rules, so it wins
});

test('SVG animation cannot turn a link into javascript: after the sanitizer has looked', () => {
  const document = parsed('<svg xmlns="http://www.w3.org/2000/svg"><a><set attributeName="href" to="javascript:alert(1)"/><animate attributeName="href" values="javascript:alert(2)"/>'
    + '<animateMotion path="M0,0"/><animateTransform attributeName="transform"/><discard/><use href="https://evil.example/x.svg#a"/><foreignObject><p>x</p></foreignObject><rect width="1" height="1"/></a></svg>');
  const names = Array.from(document.querySelectorAll('*')).map((element) => element.localName.toLowerCase());
  for (const forbidden of ['set', 'animate', 'animatemotion', 'animatetransform', 'discard', 'use', 'foreignobject']) expect(names).not.toContain(forbidden);
  expect(emailDocument('<svg><a><set attributeName="href" to="javascript:alert(1)"/></a></svg>', origin)).not.toContain('javascript');
  expect(document.querySelector('svg a')?.hasAttribute('href')).toBe(false);
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
  const document = parsed(`<img id="a" src="${proxied.replaceAll('&', '&amp;')}"><img id="b" src="https://cdn.example/logo.png"><img id="c" src="data:image/gif;base64,R0lGOD">`
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

// A white logo on a dark body background shows as a blank circle when the body's own colour is lost. The mail's
// <body> carries it (bgcolor, style, class), and only what cannot fetch or run is kept.
test('the mail body keeps its colours and classes, and loses what could fetch or run', () => {
  const document = parsed('<body bgcolor="#2a2a2a" text="#ffffff" class="dark" style="background:#111;min-height:100vh;color:#fff" onload="steal()" background="https://evil.example/b.png" data-x="1"><p>Hi</p></body>');
  const body = document.body;
  expect(body.getAttribute('bgcolor')).toBe('#2a2a2a');
  expect(body.getAttribute('text')).toBe('#ffffff');
  expect(body.className).toBe('dark');
  expect(body.getAttribute('style')).toBe('background:#111;min-height:0;color:#fff');
  for (const dropped of ['onload', 'background', 'data-x']) expect(body.hasAttribute(dropped)).toBe(false);
  const base = document.querySelector('style')?.textContent ?? '';
  expect(base).not.toMatch(/body\{[^}]*(?:background|[^-]color):/); // our own rule must not paint over the mail's colours
});

// Plain text kept inside a <div> (a chat or a note from an integration) has newlines and no markup for them.
test('text with line breaks and no markup for them keeps its breaks', () => {
  const text = '<div>Hello\n\nAttachments:\n- file: <a href="https://x.example/a.jpg">https://x.example/a.jpg</a>\n</div>';
  expect(emailDocument(text, origin)).toContain('<style>body{white-space:pre-line}</style>');
});

// pre-line keeps the newlines and folds the sender's indentation, so source formatting is not shown as spaces.
test('kept line breaks fold indentation', () => {
  const text = emailDocument('<div>\n    Hello there,\n    your order shipped.</div>', origin);
  expect(text).toContain('<style>body{white-space:pre-line}</style>');
  expect(text).not.toContain('pre-wrap}</style>');
});

test('mail that has its own structure, or only source formatting, is left to normal HTML whitespace', () => {
  const structured = [
    '<div>Hello<br>there\nagain</div>',
    '<p>Hello</p>\n<p>there\nagain</p>',
    '<table><tr><td>Hello\nthere</td></tr></table>',
    '<div>\n  <div>Hello there</div>\n  <div>again</div>\n</div>',
    '<div>A long paragraph that the sender\'s editor wrapped\n</div><div>x</div><div>\n<span>y</span>\n</div>',
    '<div>One line, no breaks</div>',
    // Hard-wrapped by the sender's generator: its newlines are not the author's lines.
    '<div>Dear customer, thank you for your order and for the patience you showed while we\nshipped it today by courier, the tracking number follows in a moment.</div>',
    '<span>Dear customer, thank you for your order and for the patience you showed while we\nshipped it today.</span>',
  ];
  for (const html of structured) expect(emailDocument(html, origin), html).not.toContain('body{white-space:pre-line}');
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
