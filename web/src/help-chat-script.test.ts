import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { afterEach, expect, test, vi } from 'vitest';

// The help center's chat script (server/internal/channels/helpchat/web/chat.js) runs on the help center's
// own pages, not in this app; this runs it in a page of the language it is given, asks one question and
// reads the answer, the way an end user does. Its words come from the server (helpcenter.ChatText).
const script = readFileSync(join(__dirname, '../../server/internal/channels/helpchat/web/chat.js'), 'utf8');

const french = {
  aiTag: 'IA', questionAndAnswer: 'Question et réponse', youAsked: 'Vous avez demandé :', askName: 'Demander à {name}',
  askQuestionAbout: 'Posez une question sur {name}', sendQuestion: 'Envoyer la question', readyOne: 'Réponse prête, 1 source.', source: 'Source',
};

afterEach(() => {
  vi.unstubAllGlobals();
  document.body.textContent = '';
  document.documentElement.lang = '';
});

test('the chat page asks a question and shows the answer in the language of the page', async () => {
  document.documentElement.lang = 'fr';
  const config = { surface: 'page', name: 'Acme', siteTitle: 'Acme help', home: '/fr', search: '/search', api: '/chat', channel: 'c1', page: '/chat?channel=c1&lang=fr',
    trial: false, greeting: '', suggestions: [], text: french };
  const mount = document.createElement('div');
  mount.id = 'chat';
  mount.setAttribute('data-config', JSON.stringify(config));
  document.body.appendChild(mount);
  const asked: { method: string; url: string; body?: string }[] = [];
  vi.stubGlobal('fetch', vi.fn((url: string, init: RequestInit) => {
    asked.push({ method: init.method ?? 'GET', url, body: init.body as string | undefined });
    if (url.includes('/chat/session')) return Promise.resolve(new Response(JSON.stringify({ turns: [], idle_hours: 0 }), { status: 200 }));
    const events = [{ type: 'turn', turn_id: 't1' }, { type: 'delta', text: 'Elles partent en 2 jours [#1].' },
      { type: 'sources', sources: [{ number: 1, url: '/fr/a/1', title: 'Délais', folder: 'Commandes' }] }, { type: 'done', status: 'answered', links_allowed: [] }];
    return Promise.resolve(new Response(events.map((event) => JSON.stringify(event)).join('\n') + '\n', { status: 200 }));
  }));
  new Function(script)();
  await vi.waitFor(() => expect(asked.some((call) => call.url.includes('/chat/session'))).toBe(true));

  const box = document.querySelector('textarea')!;
  expect(box.placeholder).toBe('Posez une question sur Acme');
  box.value = 'Quand ma commande part-elle ?';
  box.dispatchEvent(new Event('input'));
  box.closest('form')!.dispatchEvent(new Event('submit', { cancelable: true }));

  await vi.waitFor(() => expect(document.querySelector('.sources')).not.toBeNull());
  const turn = document.querySelector('article.turn')!;
  expect(turn.getAttribute('aria-label')).toBe('Question et réponse');
  expect(turn.querySelector('.q')!.textContent).toBe('Vous avez demandé : Quand ma commande part-elle ?');
  expect(turn.querySelector('.a-body')!.textContent).toContain('Elles partent en 2 jours.');
  const turns = asked.find((call) => call.method === 'POST' && call.url.includes('/chat/turns'))!;
  expect(new URL(turns.url, 'https://help.example').searchParams.get('lang')).toBe('fr');
  expect(JSON.parse(turns.body!)).toMatchObject({ question: 'Quand ma commande part-elle ?', surface: 'help_center' });
});
