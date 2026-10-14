import { expect, test } from 'vitest';
import { originalDocument } from './message-item';

test('the original-message document cannot navigate, submit or load anything', () => {
  const html = '<meta http-equiv="refresh" content="0;url=https://evil.example/r"><base href="https://evil.example/">'
    + '<link rel="stylesheet" href="https://evil.example/s.css"><p onclick="x()">Hi <a href="https://evil.example/a" target="_top">link</a></p>'
    + '<form action="https://evil.example/f"><button>go</button></form><img src="https://evil.example/p.gif" srcset="https://evil.example/2x.gif 2x">'
    + '<iframe src="https://evil.example/i"></iframe><img src="data:image/png;base64,AAAA">';
  const document = originalDocument(html);
  for (const forbidden of ['evil.example', 'refresh', '<base', '<form', '<iframe', 'onclick', 'href=']) {
    expect(document).not.toContain(forbidden);
  }
  expect(document).toContain('Hi');
  expect(document).toContain('link');
  expect(document).toContain('data:image/png;base64,AAAA');
  for (const directive of ["default-src 'none'", "form-action 'none'", "base-uri 'none'", "frame-src 'none'"]) {
    expect(document).toContain(directive);
  }
});
