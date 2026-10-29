/* The help center's AI chat on a company's own site: <script src="<help center>/chat/widget.js"
   data-channel="<id>" async></script>. It asks the help center whether this site may show the chat
   (widget-config; anything but 200 draws nothing), then draws one button in a closed shadow root and opens
   the help center's chat page in a frame on the first press. It reads nothing of the page, sets no cookie
   on the site and sends nothing before that press. docs/user/help-chat.md. */
(function () {
  'use strict';
  var me = document.currentScript;
  if (!me || !me.src) return;
  var base = new URL('.', me.src);
  var channel = me.getAttribute('data-channel');
  var configURL = new URL('widget-config', base);
  var frameURL = new URL('../chat', base); frameURL.searchParams.set('embed', '1');
  if (channel) { configURL.searchParams.set('channel', channel); frameURL.searchParams.set('channel', channel); }
  fetch(configURL.href, { credentials: 'omit' }).then(function (response) { return response.ok ? response.json() : null; }).then(function (c) {
    if (c) draw(c);
  }, function () {});

  function draw(c) {
    var brand = c.brand, label = 'AI chat: ask ' + c.name;
    var host = document.createElement('div');
    host.setAttribute('data-colliedesk-chat-root', '');
    var root = host.attachShadow({ mode: 'closed' });
    // The button sits on the company's page, whose colours are unknown: one fixed colour (the brand's
    // accent, else ink) with a white ring, whatever the visitor's scheme. Only the panel behind the frame
    // follows the scheme.
    var css = ':host{all:initial;--a:' + (brand ? brand.accent : '#171717') + ';--ai:' + (brand ? brand.accent_ink : '#fff') + ';--bg:' + (brand ? '#fff' : '#fafafa') + '}' +
      (brand ? '' : '@media (prefers-color-scheme:dark){:host{--bg:#0a0a0a}}') +
      '.l{position:fixed;right:20px;bottom:calc(20px + env(safe-area-inset-bottom));z-index:2147483000;width:56px;height:56px;border:0;border-radius:999px;cursor:pointer;display:grid;place-items:center;background:var(--a);color:var(--ai);box-shadow:0 0 0 2px #fff,0 1px 2px rgba(0,0,0,.08),0 10px 28px -6px rgba(0,0,0,.35);transition:transform .15s ease}' +
      '.l:hover{transform:translateY(-1px)}.l:focus-visible{outline:3px solid var(--a);outline-offset:3px}' +
      '.l svg{width:26px;height:26px;fill:none;stroke:currentColor;stroke-width:1.8;stroke-linecap:round;stroke-linejoin:round}' +
      '.p{position:fixed;right:20px;bottom:88px;z-index:2147483000;width:380px;height:min(560px,calc(100dvh - 108px));border-radius:12px;overflow:hidden;background:var(--bg);box-shadow:0 1px 2px rgba(0,0,0,.08),0 20px 48px -8px rgba(0,0,0,.38);display:none}' +
      '.p.o{display:block}.p iframe{border:0;width:100%;height:100%;display:block;color-scheme:normal}' +
      '@media (max-width:480px){.p{inset:0;width:100%;height:100dvh;border-radius:0}.p.o~.l{display:none}}' +
      '@media (prefers-reduced-motion:reduce){.l{transition:none}}';
    var style = document.createElement('style'); style.textContent = css; root.appendChild(style);
    var panel = document.createElement('div'); panel.className = 'p'; panel.id = 'cd-panel';
    panel.setAttribute('role', 'dialog'); panel.setAttribute('aria-label', label);
    var btn = document.createElement('button'); btn.className = 'l'; btn.type = 'button';
    btn.setAttribute('aria-label', label); btn.setAttribute('aria-expanded', 'false'); btn.setAttribute('aria-controls', 'cd-panel');
    var NS = 'http://www.w3.org/2000/svg';
    function icon(paths) {
      while (btn.firstChild) btn.removeChild(btn.firstChild);
      var s = document.createElementNS(NS, 'svg'); s.setAttribute('viewBox', '0 0 24 24'); s.setAttribute('aria-hidden', 'true');
      paths.forEach(function (d) { var p = document.createElementNS(NS, 'path'); p.setAttribute('d', d); s.appendChild(p); });
      btn.appendChild(s);
    }
    var CHAT = ['M21 12a8 8 0 0 1-11.6 7.1L4 20l1-4.6A8 8 0 1 1 21 12Z'], X = ['M18 6 6 18', 'M6 6l12 12'];
    icon(CHAT);
    root.appendChild(panel); root.appendChild(btn);
    var frame = null;
    function open() {
      if (!frame) {
        frame = document.createElement('iframe'); frame.title = label; frame.src = frameURL.href; frame.setAttribute('allow', 'clipboard-write');
        frame.addEventListener('load', function () { try { frame.contentWindow.focus(); } catch (e) {} });
        panel.appendChild(frame);
      }
      panel.classList.add('o'); btn.setAttribute('aria-expanded', 'true'); btn.setAttribute('aria-label', 'Close chat'); icon(X);
      try { frame.contentWindow.focus(); } catch (e) {}
    }
    function close() { panel.classList.remove('o'); btn.setAttribute('aria-expanded', 'false'); btn.setAttribute('aria-label', label); icon(CHAT); btn.focus(); }
    btn.addEventListener('click', function () { if (panel.classList.contains('o')) close(); else open(); });
    // Only the chat's own frame, from the help center's origin, may ask to close it.
    window.addEventListener('message', function (e) {
      if (frame && e.source === frame.contentWindow && e.origin === frameURL.origin && e.data && e.data.colliedesk === 'close') close();
    });
    document.addEventListener('keydown', function (e) { if (e.key === 'Escape' && panel.classList.contains('o')) close(); });
    document.body.appendChild(host);
  }
})();
