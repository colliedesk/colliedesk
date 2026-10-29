/* The help center's AI chat for end users: the chat page (/chat), the widget frame (/chat?embed=1) and the
   chat box of an article page. It writes the page only with createElement and textContent (the page's
   Content-Security-Policy requires Trusted Types: no HTML string ever reaches the DOM) and talks only to
   its own help center (connect-src 'self'). docs/internals/ai-chat.md. */
(function () {
  'use strict';
  var LIMIT = 1000, COUNT_FROM = 900, POLL_EVERY = 2000, POLL_FOR = 60000;
  // The visual viewport shrinks when the on-screen keyboard opens; 100dvh does not on iOS. CSSOM is allowed.
  if (window.visualViewport) {
    var vv = function () { document.documentElement.style.setProperty('--vvh', Math.round(visualViewport.height) + 'px'); };
    visualViewport.addEventListener('resize', vv); vv();
  }

  /* ---------- tiny DOM helpers ---------- */
  var SVGNS = 'http://www.w3.org/2000/svg';
  var ICONS = {
    send: ['M12 19V5', 'M5 12l7-7 7 7'], back: ['M19 12H5', 'M12 19l-7-7 7-7'], edit: ['M12 20h9', 'M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z'],
    person: ['M20 21a8 8 0 0 0-16 0', 'M12 13a4 4 0 1 0 0-8 4 4 0 0 0 0 8Z'], expand: ['M15 3h6v6', 'M9 21H3v-6', 'M21 3l-7 7', 'M3 21l7-7'],
    open: ['M14 3h7v7', 'M10 14 21 3', 'M21 14v5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5'], close: ['M18 6 6 18', 'M6 6l12 12'],
    up: ['M7 10v11', 'M15 5.9 14 10h5.8a2 2 0 0 1 2 2.3l-1.4 7a2 2 0 0 1-2 1.7H7V10l4-8a3 3 0 0 1 4 3.9Z'],
    down: ['M17 14V3', 'M9 18.1 10 14H4.2a2 2 0 0 1-2-2.3l1.4-7A2 2 0 0 1 5.6 3H17v11l-4 8a3 3 0 0 1-4-3.9Z'],
    copy: ['M9 9h11v11H9z', 'M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1'],
    info: ['M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18Z', 'M12 11v5', 'M12 8h.01'], alert: ['M12 9v4', 'M12 17h.01', 'M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0Z'],
    check: ['M20 6 9 17l-5-5'], pause: ['M10 4H6v16h4z', 'M18 4h-4v16h4z'], search: ['M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14Z', 'M20 20l-3.5-3.5'],
    clock: ['M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18Z', 'M12 7v5l3 2']
  };
  function icon(name) {
    var s = document.createElementNS(SVGNS, 'svg');
    s.setAttribute('viewBox', '0 0 24 24'); s.setAttribute('class', 'i'); s.setAttribute('aria-hidden', 'true'); s.setAttribute('focusable', 'false');
    ICONS[name].forEach(function (d) { var p = document.createElementNS(SVGNS, 'path'); p.setAttribute('d', d); s.appendChild(p); });
    return s;
  }
  function el(tag, attrs, kids) {
    var n = document.createElement(tag);
    if (attrs) Object.keys(attrs).forEach(function (k) {
      if (k === 'text') n.textContent = attrs[k];
      else if (k === 'on') { if (attrs.on) Object.keys(attrs.on).forEach(function (e) { n.addEventListener(e, attrs.on[e]); }); }
      else if (attrs[k] !== false && attrs[k] != null) n.setAttribute(k, attrs[k] === true ? '' : attrs[k]);
    });
    (kids || []).forEach(function (c) { if (c) n.appendChild(typeof c === 'string' ? document.createTextNode(c) : c); });
    return n;
  }
  function iconBtn(name, label, on, extra) {
    var a = { type: 'button', 'class': 'icon-btn', 'aria-label': label, title: label, on: on ? { click: on } : null };
    for (var k in extra) a[k] = extra[k];
    return el('button', a, [icon(name)]);
  }
  function note(kind, iconName, strong, more, actions, role) {
    return el('div', { 'class': 'note' + (kind ? ' ' + kind : ''), role: role || null }, [icon(iconName), el('p', null, [el('strong', { text: strong }), more ? ' ' : '', more ? el('span', { 'class': 'more', text: more }) : '']),
      actions && actions.length ? el('div', { 'class': 'note-actions' }, actions) : null]);
  }

  /* ---------- the server ---------- */
  function Api(c) { this.c = c; }
  Api.prototype.url = function (path) {
    var u = new URL(this.c.api + path, location.href);
    if (this.c.channel) u.searchParams.set('channel', this.c.channel);
    if (this.c.surface === 'embed') u.searchParams.set('surface', 'widget');
    return u.href;
  };
  Api.prototype.call = function (method, path, body) {
    var init = { method: method, credentials: 'same-origin', headers: {} };
    if (body !== undefined) { init.headers['Content-Type'] = 'application/json'; init.body = JSON.stringify(body); }
    return fetch(this.url(path), init);
  };

  /* ---------- an answer's text: **bold**, lines and lists, [#n] markers, the hidden-contact sentence ---------- */
  var HIDDEN = '[contact details hidden — not in the articles]';
  function Writer(body) { this.body = body; this.block = null; this.list = null; this.text = []; this.atLineStart = true; }
  Writer.prototype.line = function () {
    this.block = null; this.atLineStart = true;
  };
  Writer.prototype.target = function (lead) {
    if (this.block) return this.block;
    var m = /^\s*(?:[-*•]|\d+[.)])\s+/.exec(lead);
    if (m) {
      if (!this.list || this.list.tagName !== (/\d/.test(m[0]) ? 'OL' : 'UL')) { this.list = el(/\d/.test(m[0]) ? 'ol' : 'ul'); this.body.appendChild(this.list); }
      this.block = el('li'); this.list.appendChild(this.block);
      return { block: this.block, skip: m[0].length };
    }
    if (lead.trim()) { this.list = null; this.block = el('p'); this.body.appendChild(this.block); }
    return { block: this.block, skip: 0 };
  };
  // add writes a piece of the answer (a checked sentence); fresh marks it for the fade-in.
  Writer.prototype.add = function (piece, fresh) {
    var self = this;
    piece.split('\n').forEach(function (part, i) {
      if (i > 0) self.line();
      if (!part) return;
      var skip = 0;
      if (self.atLineStart) {
        if (!part.trim()) return;
        var t = self.target(part); skip = t.skip;
        if (!t.block) return;
        self.atLineStart = false;
      }
      var span = el('span', { 'class': fresh ? 's-new' : null });
      // A marker before its punctuation moves after it ("depot [#3]." → "depot.[#3]").
      var text = part.slice(skip).replace(/\s+\[#/g, '[#').replace(/((?:\[#\d+\])+)([.,;:!?])/g, '$2$1');
      text.split(/(\*\*[^*]+\*\*|\[#\d+\])/).forEach(function (bit) {
        if (!bit) return;
        var m = /^\[#(\d+)\]$/.exec(bit);
        if (m) span.appendChild(el('span', { 'class': 'cite cite-wait', 'data-number': m[1], 'aria-hidden': 'true' }));
        else if (/^\*\*/.test(bit)) span.appendChild(el('strong', { text: bit.slice(2, -2) }));
        else if (bit.indexOf(HIDDEN) >= 0) bit.split(HIDDEN).forEach(function (rest, j) { if (j) span.appendChild(el('span', { 'class': 'redacted', text: HIDDEN })); if (rest) span.appendChild(document.createTextNode(rest)); });
        else span.appendChild(document.createTextNode(bit));
      });
      self.block.appendChild(span);
    });
    this.text.push(piece.replace(/\s*\[#\d+\]/g, '').replace(/\*\*/g, ''));
  };
  Writer.prototype.plain = function () { return this.text.join('').trim(); };
  // links: only the article's own links become links (links_allowed); the rest stay text.
  function linkify(body, allowed) {
    if (!allowed || !allowed.length) return;
    var walker = document.createTreeWalker(body, NodeFilter.SHOW_TEXT), nodes = [], n;
    while ((n = walker.nextNode())) nodes.push(n);
    nodes.forEach(function (node) {
      allowed.forEach(function (link) {
        var at = node.nodeValue.indexOf(link);
        if (at < 0 || !/^https?:\/\//i.test(link)) return;
        var after = node.splitText(at); after.splitText(link.length);
        after.parentNode.replaceChild(el('a', { href: link, rel: 'noopener nofollow', target: '_blank', text: link }), after);
      });
    });
  }

  /* ---------- the chat frame ---------- */
  function Chat(host, c) {
    this.host = host; this.c = c; this.api = new Api(c); this.compact = c.surface !== 'page';
    this.article = c.article || null; this.busy = false; this.handed = false;
    this.build();
  }
  Chat.prototype.name = function () { return this.c.name; };
  Chat.prototype.build = function () {
    var self = this, c = this.c;
    var root = el('div', { 'class': 'chat' + (this.compact ? ' chat-compact' : '') });
    var top = el('header', { 'class': 'chat-top' });
    function title(tag, text, id, short) {
      var name = short ? el('span', null, [el('span', { 'class': 't-long', text: text }), el('span', { 'class': 't-short', 'aria-hidden': 'true', text: short })]) : el('span', { text: text });
      return el(tag, { 'class': 'chat-title', id: id }, [name, el('span', { 'class': 'ai-tag', text: 'AI', title: 'Answers are written by AI' })]);
    }
    if (c.surface === 'page') {
      top.appendChild(el('a', { 'class': 'icon-btn chat-back', href: c.home, 'aria-label': 'Back to help center', title: 'Back to help center' }, [icon('back')]));
      top.appendChild(el('a', { 'class': 'brand', href: c.home, title: 'Back to help center' }, [c.logo ? el('img', { 'class': 'brand-logo', src: c.logo.url, alt: c.logo.alt, width: c.logo.width || null, height: c.logo.height || null }) : c.siteTitle]));
      top.appendChild(title('h1', 'Ask ' + c.name));
      this.personTop = el('button', { type: 'button', 'class': 'btn btn-quiet person', on: { click: function () { self.openHandoff(this); } } }, [icon('person'), el('span', { 'class': 'p-label', text: 'Talk to a person' })]);
      top.appendChild(el('div', { 'class': 'top-actions' }, [this.personTop,
        iconBtn('person', 'Talk to a person', function () { self.openHandoff(this); }, { 'class': 'icon-btn person-icon' }),
        el('button', { type: 'button', 'class': 'btn new', 'aria-label': 'New chat', on: { click: function () { self.confirmNew(this); } } }, [icon('edit'), el('span', { 'class': 'label', text: 'New chat' })])]));
    } else {
      top.appendChild(title(c.surface === 'embed' ? 'h1' : 'h2', c.surface === 'article' ? 'Ask about this article' : 'Ask ' + c.name, 'chat-title', c.surface === 'article' ? 'This article' : null));
      top.appendChild(iconBtn('edit', 'New chat', function () { self.confirmNew(this); }));
      if (c.surface === 'article') top.appendChild(el('a', { 'class': 'icon-btn', href: c.page, 'aria-label': 'Open in full chat', title: 'Open in full chat' }, [icon('expand')]));
      else top.appendChild(iconBtn('open', 'Open full chat in a new tab', function () { self.openFull(); }));
      top.appendChild(iconBtn('close', 'Close chat', function () { c.onClose && c.onClose(); }));
    }
    root.appendChild(top);
    if (c.surface === 'article') { this.chipRow = el('div', { 'class': 'chip-row' }); root.appendChild(this.chipRow); this.renderChip(); }
    this.scroll = el('div', { 'class': 'chat-scroll' });
    this.thread = el('div', { 'class': 'thread' });
    this.scroll.appendChild(this.thread);
    root.appendChild(this.scroll);
    this.foot = el('div', { 'class': 'composer-wrap' });
    root.appendChild(this.foot);
    this.live = el('div', { 'class': 'sr-live', 'aria-live': 'polite', 'aria-atomic': 'false', 'aria-relevant': 'additions' });
    root.appendChild(this.live);
    this.host.appendChild(root);
    this.root = root;
    this.buildComposer();
  };
  Chat.prototype.renderChip = function () {
    var self = this; this.chipRow.textContent = '';
    if (!this.article) { this.chipRow.hidden = true; return; }
    this.chipRow.hidden = false;
    this.chipRow.appendChild(el('span', { 'class': 'chip' }, [el('b', { text: 'Asking about:' }), el('span', { text: this.article.title, title: this.article.title }),
      iconBtn('close', 'Stop asking about “' + this.article.title + '”', function () { self.article = null; self.renderChip(); self.input.focus(); self.say('Questions are no longer about this article.'); })]));
  };
  Chat.prototype.placeholder = function () { return this.compact ? 'Ask a question' : 'Ask a question about ' + this.c.name; };
  Chat.prototype.buildComposer = function () {
    var self = this; this.foot.textContent = '';
    var inner = el('div', { 'class': 'composer-in' });
    this.waitLine = el('p', { 'class': 'wait-line', hidden: true, role: 'status' });
    inner.appendChild(this.waitLine);
    var id = 'q-' + this.c.surface;
    this.input = el('textarea', { id: id, rows: '1', placeholder: this.placeholder(), 'aria-describedby': id + '-note ' + id + '-count', autocomplete: 'off', enterkeyhint: 'send' });
    this.sendBtn = el('button', { type: 'submit', 'class': 'send', 'aria-label': 'Send question', disabled: true }, [icon('send')]);
    this.form = el('form', { 'class': 'composer', on: { submit: function (e) { e.preventDefault(); self.submit(); } } },
      [el('label', { 'for': id, 'class': 'visually-hidden', text: 'Your question' }), this.input, this.sendBtn]);
    inner.appendChild(this.form);
    this.count = el('span', { 'class': 'count', id: id + '-count', hidden: true });
    var text = this.compact ? 'AI answers can be wrong. Kept on this browser for 30 days.' : 'AI answers from ' + this.c.name + '’s help articles and can be wrong. This chat is kept on this browser for 30 days.';
    inner.appendChild(el('div', { 'class': 'composer-meta' }, [el('p', { id: id + '-note', text: text }),
      el('button', { type: 'button', 'class': 'link person', text: this.personLabel(), on: { click: function () { self.openHandoff(this); } } }), this.count]));
    this.foot.appendChild(inner);
    this.input.addEventListener('input', function () { self.onInput(); });
    this.input.addEventListener('keydown', function (e) {
      if (e.key === 'Enter' && !e.shiftKey && !e.isComposing && e.keyCode !== 229) { e.preventDefault(); self.submit(); }
    });
    // Warm the AI's cache of the articles as the end user starts: the first answer comes sooner.
    this.input.addEventListener('focus', function () { if (!self.warmed) { self.warmed = true; self.api.call('POST', '/prewarm').catch(function () {}); } }, { once: true });
    if (this.locked) { this.input.disabled = true; this.input.placeholder = this.lockText; }
  };
  Chat.prototype.onInput = function () {
    var n = this.input.value.length, over = n > LIMIT;
    this.count.hidden = n < COUNT_FROM;
    this.count.textContent = n.toLocaleString('en') + ' / ' + LIMIT.toLocaleString('en');
    this.count.classList.toggle('over', over);
    this.form.classList.toggle('too-long', over);
    this.input.setAttribute('aria-invalid', over ? 'true' : 'false');
    if (!this.waiting) {
      this.waitLine.hidden = !over;
      if (over) { this.waitLine.textContent = ''; this.waitLine.appendChild(icon('alert')); this.waitLine.appendChild(document.createTextNode('Shorten your question to 1,000 characters.')); }
    }
    this.sendBtn.disabled = !this.input.value.trim() || over || this.busy || !!this.waiting || !!this.locked;
  };
  Chat.prototype.lock = function (text) { this.locked = true; this.lockText = text; this.input.disabled = true; this.input.placeholder = text; this.onInput(); };
  Chat.prototype.unlock = function () { this.locked = false; this.input.disabled = false; this.input.placeholder = this.placeholder(); this.onInput(); };
  Chat.prototype.say = function (t) { var l = this.live; setTimeout(function () { l.appendChild(el('p', { text: t })); }, 30); };
  Chat.prototype.hush = function () { this.live.textContent = ''; };
  Chat.prototype.bottom = function () { this.scroll.scrollTop = this.scroll.scrollHeight; };
  Chat.prototype.clear = function () { this.thread.textContent = ''; };

  Chat.prototype.welcome = function () {
    var self = this, list = el('ul', { 'class': 'suggest', 'aria-label': 'Suggested questions' });
    (this.c.suggestions || []).forEach(function (s) { list.appendChild(el('li', null, [el('button', { type: 'button', text: s, on: { click: function () { self.ask(s); } } })])); });
    this.thread.appendChild(el('section', { 'class': 'welcome', 'aria-labelledby': 'w-' + this.c.surface }, [
      el('h2', { id: 'w-' + this.c.surface, text: this.compact ? 'Ask ' + this.c.name : 'How can we help?' }),
      el('p', { 'class': 'who' }, [el('span', { 'class': 'ai-tag', text: 'AI' }), 'AI assistant · answers from ' + this.c.name + '’s help articles']),
      this.c.greeting ? el('p', { 'class': 'greet', text: this.c.greeting }) : null, (this.c.suggestions || []).length ? list : null]));
  };

  /* ---------- a turn ---------- */
  Chat.prototype.turnEl = function (q) {
    var t = el('article', { 'class': 'turn', 'aria-label': 'Question and answer' });
    t.appendChild(el('p', { 'class': 'q' }, [el('span', { 'class': 'visually-hidden', text: 'You asked: ' }), q]));
    var a = el('div', { 'class': 'a' }), body = el('div', { 'class': 'a-body' });
    a.appendChild(body); t.appendChild(a); this.thread.appendChild(t);
    return { t: t, a: a, body: body, q: q, w: new Writer(body), id: null };
  };
  // sources: numbered 1..k in their order; a marker becomes the source's link, one of no source goes.
  Chat.prototype.finishSources = function (turn, sources) {
    var self = this, nums = {}, list = el('ul', { 'class': 'tabs' });
    sources.forEach(function (s, i) { nums[s.number] = { n: i + 1, s: s }; });
    Array.prototype.slice.call(turn.body.querySelectorAll('.cite-wait')).forEach(function (cite) {
      var hit = nums[cite.getAttribute('data-number')];
      if (!hit) { cite.remove(); return; }
      cite.replaceWith(el('a', { 'class': 'cite', href: hit.s.url, tabindex: '-1', target: self.c.surface === 'embed' ? '_blank' : null, 'aria-label': 'Source ' + hit.n + ': ' + hit.s.title, text: String(hit.n) }));
    });
    if (!sources.length) return;
    sources.forEach(function (s, i) {
      list.appendChild(el('li', null, [el('a', { 'class': 'tab', href: s.url, title: s.title, target: self.c.surface === 'embed' ? '_blank' : null }, [
        el('span', { 'class': 'cite', 'aria-hidden': 'true', text: String(i + 1) }), el('span', { 'class': 'tab-folder', text: s.folder }), el('span', { 'class': 'tab-title', text: s.title })])]));
    });
    turn.a.appendChild(el('div', { 'class': 'sources' }, [el('h3', { 'class': 'sources-label', text: sources.length === 1 ? 'Source' : 'Sources' }), list]));
  };
  Chat.prototype.tools = function (turn, feedback) {
    var self = this, done = el('span', { 'class': 'done', role: 'status' });
    var up = iconBtn('up', 'Helpful', null, { 'aria-pressed': String(feedback === 'helpful') });
    var down = iconBtn('down', 'Not helpful', null, { 'aria-pressed': String(feedback === 'not_helpful') });
    function rate(btn, other, rating) {
      return function () {
        var on = btn.getAttribute('aria-pressed') !== 'true';
        btn.setAttribute('aria-pressed', String(on)); if (on) other.setAttribute('aria-pressed', 'false');
        done.textContent = on ? 'Thanks for the feedback' : '';
        self.api.call(on ? 'PUT' : 'DELETE', '/turns/' + encodeURIComponent(turn.id) + '/feedback', on ? { rating: rating } : undefined).catch(function () {});
      };
    }
    up.addEventListener('click', rate(up, down, 'helpful')); down.addEventListener('click', rate(down, up, 'not_helpful'));
    var copy = iconBtn('copy', 'Copy answer', function () { try { navigator.clipboard.writeText(turn.w.plain()); } catch (e) {} done.textContent = 'Copied'; setTimeout(function () { done.textContent = ''; }, 2000); });
    turn.a.appendChild(el('div', { 'class': 'a-tools' }, turn.id ? [up, down, copy, done] : [copy, done]));
  };
  Chat.prototype.personLabel = function () { return this.handed ? 'Message the team again' : 'Talk to a person'; };
  Chat.prototype.personBtn = function () { var self = this; return el('button', { type: 'button', 'class': 'btn', on: { click: function () { self.openHandoff(this); } } }, [icon('person'), this.personLabel()]); };
  // The end of an answer, streamed or read back.
  Chat.prototype.outcome = function (turn, status, sources, redacted, feedback) {
    var self = this;
    if (status === 'over_limit') {
      var list = el('ul', { 'class': 'index' });
      sources.forEach(function (s) { list.appendChild(el('li', null, [el('a', { href: s.url, target: self.c.surface === 'embed' ? '_blank' : null }, [s.title, el('small', { text: s.folder })])])); });
      var paused = note('', 'pause', 'AI answers are paused for now.', sources.length ? 'These articles match your question:' : 'You can still read the help articles.', null);
      if (sources.length) paused.appendChild(list);
      paused.appendChild(el('div', { 'class': 'note-actions' }, [self.personBtn()]));
      turn.a.appendChild(paused);
      return;
    }
    this.finishSources(turn, sources);
    if (status === 'no_source' || redacted) {
      turn.a.appendChild(note('note-inline', 'search', status === 'no_source' ? 'Couldn’t find this in the help articles.' : 'Some contact details were not in the articles.',
        redacted ? 'The team can give you their contact details.' : '', [self.personBtn()]));
    }
    this.tools(turn, feedback);
  };
  Chat.prototype.failed = function (turn) {
    var self = this;
    turn.a.appendChild(note('note-problem', 'alert', 'Something went wrong.', 'The answer did not finish.', [
      el('button', { type: 'button', 'class': 'btn btn-primary', text: 'Try again', on: { click: function () { this.closest('.note').remove(); self.ask(turn.q); } } }), self.personBtn()], 'alert'));
    this.busy = false; this.onInput(); this.bottom();
  };

  // ask sends a question and draws the answer as its events arrive (NDJSON).
  Chat.prototype.ask = function (q) {
    var self = this;
    if (this.busy || this.locked || !q) return;
    var welcome = this.thread.querySelector('.welcome'); if (welcome) welcome.hidden = true;
    var turn = this.turnEl(q);
    var dots = el('div', { 'class': 'typing', role: 'img', 'aria-label': 'Answer on its way' }, [el('i'), el('i'), el('i')]);
    turn.a.appendChild(dots);
    this.busy = true; this.input.value = ''; this.onInput(); this.bottom();
    this.hush(); this.say('Answer on its way');
    var body = { question: q, surface: this.c.surface === 'article' ? 'article' : this.c.surface === 'embed' ? 'widget' : 'help_center' };
    if (this.article) body.viewing_article_number = this.article.number;
    var sources = [], ended = false;
    // Not sent: the question goes back in the box, and a new chat shows its welcome again.
    function back(problem) {
      turn.t.remove(); self.busy = false; self.input.value = q; self.onInput();
      var old = self.thread.querySelector('.note-sent-back'); if (old) old.remove();
      problem.classList.add('note-sent-back');
      if (welcome && !self.thread.querySelector('.turn')) welcome.hidden = false;
      self.thread.appendChild(problem); self.bottom();
    }
    this.api.call('POST', '/turns', body).then(function (response) {
      if (response.status === 404) { turn.t.remove(); self.busy = false; return self.unavailable(); }
      if (response.status === 429) { back(note('', 'clock', 'Too many questions, wait a minute.', 'Your question was not sent. Ask it again when the timer ends.', null, 'status')); return self.wait(response.headers.get('Retry-After')); }
      if (response.status === 409) { turn.t.remove(); self.busy = false; return self.chatFull(); }
      if (!response.ok || !response.body) return back(note('note-problem', 'alert', 'Couldn’t send.', 'Check your connection and try again. Your question is still in the box.', null, 'alert'));
      var reader = response.body.getReader(), decoder = new TextDecoder(), buffer = '';
      function handle(event) {
        if (event.type === 'turn') { turn.id = event.turn_id; return; }
        if (event.type === 'delta') {
          if (dots.parentNode) dots.remove();
          turn.w.add(event.text, true);
          var last = turn.body.lastElementChild; Array.prototype.forEach.call(turn.body.querySelectorAll('.caret'), function (n) { n.classList.remove('caret'); }); if (last) last.classList.add('caret');
          self.say(event.text.replace(/\s*\[#\d+\]/g, '')); self.bottom(); return;
        }
        if (event.type === 'sources') { sources = event.sources || []; return; }
        if (event.type === 'done' || event.type === 'error') {
          ended = true; if (dots.parentNode) dots.remove();
          Array.prototype.forEach.call(turn.body.querySelectorAll('.caret'), function (n) { n.classList.remove('caret'); });
          if (event.type === 'error') return self.failed(turn);
          linkify(turn.body, event.links_allowed);
          self.outcome(turn, event.status, sources, event.redacted);
          self.busy = false; self.onInput(); self.bottom();
          self.say(event.status === 'answered' ? 'Answer ready, ' + sources.length + (sources.length === 1 ? ' source.' : ' sources.') : event.status === 'over_limit' ? 'AI answers are paused.' : 'Answer ready. Couldn’t find this in the help articles.');
        }
      }
      function pump() {
        return reader.read().then(function (part) {
          if (part.done) { if (!ended) throw new Error('cut'); return; }
          buffer += decoder.decode(part.value, { stream: true });
          var lines = buffer.split('\n'); buffer = lines.pop();
          lines.forEach(function (line) { if (line.trim()) handle(JSON.parse(line)); });
          return pump();
        });
      }
      return pump().catch(function () { if (!ended) self.reconnect(turn, dots); });
    }, function () { back(note('note-problem', 'alert', 'Couldn’t send.', 'Check your connection and try again. Your question is still in the box.', null, 'alert')); });
  };
  // reconnect reads the stored answer back every 2 s for a minute once a stream breaks.
  Chat.prototype.reconnect = function (turn, dots) {
    var self = this, started = Date.now();
    if (dots && dots.parentNode) dots.remove();
    var line = el('p', { 'class': 'status-line', role: 'status' }, [el('span', { 'class': 'spin', 'aria-hidden': 'true' }), 'Reconnecting…']);
    turn.a.appendChild(line); this.bottom();
    function poll() {
      if (!turn.id || Date.now() - started > POLL_FOR) { line.remove(); return self.failed(turn); }
      self.api.call('GET', '/turns/' + encodeURIComponent(turn.id)).then(function (response) {
        if (!response.ok) throw new Error('not yet');
        return response.json();
      }).then(function (stored) {
        if (stored.status === 'streaming') return setTimeout(poll, POLL_EVERY);
        line.remove(); turn.body.textContent = ''; turn.w = new Writer(turn.body);
        self.drawStored(turn, stored); self.busy = false; self.onInput(); self.say('Reconnected. Answer ready.');
      }).catch(function () { setTimeout(poll, POLL_EVERY); });
    }
    setTimeout(poll, POLL_EVERY);
  };
  Chat.prototype.drawStored = function (turn, stored) {
    turn.id = stored.id;
    if (stored.status === 'failed') { if (stored.answer) turn.w.add(stored.answer, false); return this.failed(turn); }
    if (stored.answer) turn.w.add(stored.answer, false);
    this.outcome(turn, stored.status, stored.sources || [], stored.redacted, stored.feedback);
  };
  Chat.prototype.submit = function () {
    var q = this.input.value.trim();
    if (!q || q.length > LIMIT || this.busy || this.waiting || this.locked) return;
    this.ask(q);
  };

  /* ---------- states ---------- */
  Chat.prototype.wait = function (retryAfter) {
    var self = this, left = Math.max(1, parseInt(retryAfter, 10) || 60);
    this.waiting = true; this.waitLine.hidden = false;
    function tick() {
      self.waitLine.textContent = ''; self.waitLine.appendChild(icon('clock'));
      self.waitLine.appendChild(document.createTextNode('You can ask again in ' + Math.floor(left / 60) + ':' + String(left % 60).padStart(2, '0')));
      if (left-- <= 0) { self.waiting = false; self.waitLine.hidden = true; self.onInput(); self.say('You can ask again.'); return; }
      setTimeout(tick, 1000);
    }
    tick(); this.onInput();
  };
  Chat.prototype.unavailable = function () {
    this.thread.appendChild(note('note-problem', 'pause', 'AI chat isn’t available right now.', 'You can still read the help articles.',
      [el('a', { 'class': 'btn', href: this.c.home, text: 'Go to the help center' })], 'alert'));
    this.lock('AI chat isn’t available right now');
  };
  Chat.prototype.chatFull = function () {
    var self = this;
    this.thread.appendChild(note('', 'info', 'This chat is long.', 'Start a new chat to keep asking.',
      [el('button', { type: 'button', 'class': 'btn btn-primary', on: { click: function () { self.confirmNew(this); } } }, [icon('edit'), 'New chat'])], 'status'));
    this.lock('Start a new chat to keep asking');
  };
  Chat.prototype.history = function (turns) {
    var self = this;
    turns.forEach(function (stored) { var turn = self.turnEl(stored.question || ''); self.drawStored(turn, stored); });
  };
  // resume: a chat idle more than a day shows nothing of itself until the end user chooses Continue.
  Chat.prototype.resume = function (session) {
    var self = this, n = session.turns.length, days = Math.max(1, Math.round(session.idle_hours / 24));
    var ghost = el('div', { 'class': 'ghost', 'aria-hidden': 'true' }, [el('i'), el('i'), el('i'), el('i')]);
    this.thread.appendChild(el('section', { 'class': 'resume', 'aria-labelledby': 'resume-' + this.c.surface }, [
      el('h2', { id: 'resume-' + this.c.surface, text: 'Continue your previous chat?' }),
      el('p', { text: n + (n === 1 ? ' question' : ' questions') + ' · last active ' + days + (days === 1 ? ' day' : ' days') + ' ago. It stays hidden until you continue.' }), ghost,
      el('div', { 'class': 'resume-actions' }, [
        el('button', { type: 'button', 'class': 'btn btn-primary', text: 'Continue', on: { click: function () { self.clear(); self.unlock(); self.history(session.turns); self.bottom(); self.input.focus(); self.say('Previous chat opened.'); } } }),
        el('button', { type: 'button', 'class': 'btn', text: 'New chat', on: { click: function () { self.startOver(); } } })])]));
    this.lock('Choose Continue or New chat first');
  };
  Chat.prototype.load = function () {
    var self = this;
    return this.api.call('GET', '/session').then(function (response) {
      if (response.status === 404) return self.unavailable();
      if (response.status !== 200) return self.welcome();
      return response.json().then(function (session) {
        if (!session.turns.length) return self.welcome();
        if (session.idle_hours >= 24) return self.resume(session);
        self.history(session.turns); requestAnimationFrame(function () { self.bottom(); });
      });
    }).catch(function () { self.welcome(); });
  };
  Chat.prototype.startOver = function () {
    var self = this;
    this.api.call('POST', '/session/end').catch(function () {}).then(function () {
      self.clear(); self.handed = false; self.waiting = false; self.busy = false; self.locked = false;
      self.buildComposer(); self.welcome(); self.input.focus(); self.say('New chat started.');
    });
  };
  Chat.prototype.confirmNew = function (opener) {
    var self = this;
    var d = el('dialog', { 'class': 'confirm', 'aria-labelledby': 'cn-t', 'aria-describedby': 'cn-d' }, [
      el('h2', { id: 'cn-t', text: 'Start a new chat?' }), el('p', { id: 'cn-d', text: 'This one will be cleared from this browser.' }),
      el('div', { 'class': 'handoff-actions' }, [el('button', { type: 'button', 'class': 'btn', text: 'Cancel', on: { click: function () { d.close(); } } }),
        el('button', { type: 'button', 'class': 'btn btn-primary', text: 'Start new chat', on: { click: function () { d.close(); self.startOver(); } } })])]);
    d.addEventListener('close', function () { d.remove(); if (document.contains(opener) && !self.root.contains(document.activeElement)) opener.focus(); });
    this.root.appendChild(d); d.showModal();
    d.querySelector('.btn').focus();
  };
  // openFull moves the chat from a site's frame to the help center's own chat page, in a new tab.
  Chat.prototype.openFull = function () {
    var self = this;
    this.api.call('POST', '/transfer', {}).then(function (response) { return response.ok ? response.json() : { url: self.c.page }; })
      .catch(function () { return { url: self.c.page }; }).then(function (to) {
        window.open(to.url, '_blank', 'noopener');
        // The chat moves to the new tab: this frame starts over.
        if (/[?&]t=/.test(to.url)) {
          self.clear(); self.welcome();
          self.thread.appendChild(note('', 'info', 'This chat continues in the new tab.', '', null, 'status'));
        }
      });
  };

  /* ---------- Talk to a person ---------- */
  Chat.prototype.openHandoff = function (opener) {
    var self = this, id = 'h-' + this.c.surface;
    this.handoffOpener = opener;
    var email = el('input', { id: id + '-email', type: 'email', autocomplete: 'email', inputmode: 'email', required: true, maxlength: '254', 'aria-describedby': id + '-email-err' });
    var name = el('input', { id: id + '-name', type: 'text', autocomplete: 'name', maxlength: '120' });
    // A second hand-off of this page keeps what was typed the first time (in memory only).
    email.value = this.sentEmail || ''; name.value = this.sentName || '';
    var text = el('textarea', { id: id + '-note', rows: '3', maxlength: '2000', 'aria-describedby': id + '-note-hint' });
    var err = el('p', { 'class': 'field-error', id: id + '-email-err', hidden: true });
    var formErr = el('div', { 'class': 'form-error', role: 'alert', hidden: true });
    var hint = el('p', { 'class': 'field-hint', id: id + '-note-hint', text: '0 / 2,000' });
    text.addEventListener('input', function () { hint.textContent = text.value.length.toLocaleString('en') + ' / 2,000'; });
    var sendBtn = el('button', { type: 'submit', 'class': 'btn btn-primary', text: 'Send to ' + this.c.name });
    function fieldError(message) { err.hidden = false; err.textContent = ''; err.appendChild(icon('alert')); err.appendChild(document.createTextNode(message)); email.setAttribute('aria-invalid', 'true'); email.focus(); }
    function formError(message) { formErr.hidden = false; formErr.textContent = ''; formErr.appendChild(icon('alert')); formErr.appendChild(document.createTextNode(message)); }
    var form = el('form', { 'class': 'handoff', novalidate: true, 'aria-labelledby': id + '-t', on: { submit: function (e) {
      e.preventDefault();
      var address = email.value.trim();
      if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(address)) return fieldError('Enter an email address like name@example.com.');
      err.hidden = true; email.setAttribute('aria-invalid', 'false'); formErr.hidden = true;
      sendBtn.disabled = true; sendBtn.textContent = 'Sending…';
      self.api.call('POST', '/handoff', { email: address, name: name.value.trim() || undefined, note: text.value.trim() || undefined }).then(function (response) {
        if (response.status === 201) return response.json().then(function (sent) { self.sentEmail = address; self.sentName = name.value.trim(); self.closeHandoff(false); self.handedOff(sent); });
        sendBtn.disabled = false; sendBtn.textContent = 'Send to ' + self.c.name;
        if (response.status === 422) return fieldError('Enter an email address like name@example.com.');
        if (response.status === 429) return formError('Too many requests, try again in a minute.');
        formError('Couldn’t send. Try again in a moment.');
      }, function () { sendBtn.disabled = false; sendBtn.textContent = 'Send to ' + self.c.name; formError('Couldn’t send. Check your connection and try again.'); });
    } } }, [
      el('h2', { id: id + '-t', text: this.personLabel() }),
      el('p', { text: 'We send this chat to the ' + this.c.name + ' team with your note. You can keep asking the AI meanwhile.' }),
      formErr,
      el('div', { 'class': 'field' }, [el('label', { 'for': email.id, text: 'Email' }), email, err]),
      el('div', { 'class': 'field' }, [el('label', { 'for': name.id }, ['Name ', el('span', { text: '(optional)' })]), name]),
      el('div', { 'class': 'field' }, [el('label', { 'for': text.id }, ['Anything to add? ', el('span', { text: '(optional)' })]), text, hint]),
      el('div', { 'class': 'handoff-actions' }, [el('button', { type: 'button', 'class': 'btn btn-quiet', text: 'Cancel', on: { click: function () { self.closeHandoff(true); } } }), sendBtn])]);
    form.addEventListener('keydown', function (e) { if (e.key === 'Escape') { e.stopPropagation(); e.preventDefault(); self.closeHandoff(true); } });
    this.foot.textContent = ''; this.foot.appendChild(form); this.handoffOpen = true;
    email.focus();
  };
  Chat.prototype.closeHandoff = function () {
    this.handoffOpen = false; this.buildComposer(); this.onInput(); this.input.focus();
  };
  Chat.prototype.handedOff = function (sent) {
    this.handed = true; this.buildComposer(); this.onInput();
    var label = this.root.querySelector('.top-actions .p-label'); if (label) label.textContent = this.personLabel();
    var message = this.c.trial ? 'Sent. ' + this.c.name + ' has your message.' : 'Sent. ' + this.c.name + ' will reply by email.';
    this.thread.appendChild(el('div', { 'class': 'note note-ok', role: 'status' }, [icon('check'), el('p', null, [el('strong', { text: message }), ' ',
      el('span', { 'class': 'more' }, ['Reference ', el('span', { 'class': 'ref', text: '#' + sent.reference }), '. You can keep asking here.'])])]));
    this.bottom();
  };

  /* ---------- where it runs ---------- */
  function config(node) { try { return JSON.parse(node.getAttribute('data-config') || '{}'); } catch (e) { return {}; } }

  var page = document.getElementById('chat');
  if (page) {
    var c = config(page);
    if (c.surface === 'embed') {
      // The site's frame: its × and Esc ask widget.js to close the panel; the message carries no data.
      c.onClose = function () { parent.postMessage({ colliedesk: 'close' }, '*'); };
      document.addEventListener('keydown', function (e) { if (e.key === 'Escape' && !e.defaultPrevented && !document.querySelector('dialog[open]')) c.onClose(); });
    }
    var chat = new Chat(page, c);
    // With the chat drawn, the skip link leads to its question box.
    var skip = document.querySelector('.skip'); if (skip) skip.setAttribute('href', '#' + chat.input.id);
    chat.load().then(function () { if (!chat.locked) chat.input.focus(); });
    return;
  }

  // An article page: the button opens the chat beside the article (≥ 768 px) or as a sheet over it.
  var launch = document.getElementById('ask-launch'), box = document.getElementById('chatbox');
  if (!launch || !box) return;
  var bc = config(box), wide = matchMedia('(min-width: 48rem)'), boxChat = null;
  bc.surface = 'article';
  bc.article = { number: Number(box.getAttribute('data-article-number')), title: box.getAttribute('data-article-title') };
  var outside = [document.querySelector('.masthead'), document.getElementById('main'), document.querySelector('.foot'), document.querySelector('.skip')].filter(Boolean);
  function mode() {
    var open = box.classList.contains('open'), sheet = open && !wide.matches;
    document.body.classList.toggle('docked', open && wide.matches);
    // Docked beside the article it is a landmark, not a dialog; under 768 px it is a modal sheet.
    if (wide.matches) { box.setAttribute('role', 'complementary'); box.removeAttribute('aria-modal'); launch.removeAttribute('aria-haspopup'); }
    else { box.setAttribute('role', 'dialog'); box.setAttribute('aria-modal', 'true'); launch.setAttribute('aria-haspopup', 'dialog'); }
    outside.forEach(function (n) { if (sheet) n.setAttribute('inert', ''); else n.removeAttribute('inert'); });
  }
  // restored: the box opens again by itself on the next article, docked only and without taking the
  // focus (the reader starts at the page, its skip link first).
  function open(restored) {
    if (!boxChat) { bc.onClose = close; boxChat = new Chat(box, bc); boxChat.load(); }
    box.classList.add('open'); launch.setAttribute('aria-expanded', 'true'); mode();
    try { sessionStorage.setItem('cd_chat_open', '1'); } catch (e) {}
    if (restored !== true && !document.querySelector('dialog[open]') && !boxChat.handoffOpen) boxChat.input.focus();
  }
  function close() {
    box.classList.remove('open'); launch.setAttribute('aria-expanded', 'false'); mode();
    try { sessionStorage.removeItem('cd_chat_open'); } catch (e) {}
    launch.focus();
  }
  launch.hidden = false; mode();
  launch.addEventListener('click', function () { open(); });
  box.addEventListener('keydown', function (e) { if (e.key === 'Escape' && !e.defaultPrevented && !box.querySelector('dialog[open]')) { e.preventDefault(); close(); } });
  wide.addEventListener('change', mode);
  var reopen = false; try { reopen = sessionStorage.getItem('cd_chat_open') === '1'; } catch (e) {}
  // Under 768 px the box is a sheet over the article: a source opened from it shows the article.
  if (reopen && wide.matches) open(true);
})();
