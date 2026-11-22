import { renderHook } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { useImageAnnotation } from './article-annotate';
import type { AnnotationState } from './article-annotate';
import { article, ids, json, refusal, serve } from './kb.test.fixture';
import type { KBArticle } from './kb-api';

const copyId = 'aaaaaaaa-aaaa-4aaa-8aaa-000000000031';
const item = (id: string, name: string) => ({
  id, kind: 'image', content_type: 'image/png', size: 2_048, sha256: 'c'.repeat(64), url: `/api/v1/kb/media/${id}`, name, alt: '',
  width: 640, height: 480, duration_ms: null, codecs: [], thumbnail_url: null, poster_url: null, kept: false, deleted_at: null,
  created_at: '2026-11-01T00:00:00Z', updated_at: '2026-11-01T00:00:00Z', usage: { articles: 1, trashed_articles: 0, older_versions: 0, help_center: [] },
});
const details = { ...item(ids.image, 'Settings.png'), articles: [], earlier_versions: [] };
const notInArticle = 'The image is not in this article any more. Reload the article and try again.';

function annotate(trash: () => Response) {
  const calls = serve({
    [`GET /api/v1/kb/media/${ids.image}/details`]: () => json(200, details),
    'POST /api/v1/kb/images': () => json(201, item(copyId, 'Settings (annotated).png')),
    [`POST /api/v1/kb/media/${ids.image}/replace`]: () => refusal(409, 'media_not_in_article', notInArticle),
    'POST /api/v1/kb/media/trash': trash,
  });
  const state: AnnotationState = { article: article() as unknown as KBArticle, blocked: false, unsaved: () => false, reload: vi.fn(() => Promise.resolve()), notice: vi.fn() };
  const { result } = renderHook(() => useImageAnnotation(state));
  const save = result.current.save(`/api/v1/kb/images/${ids.image}`, new Blob(['png'], { type: 'image/png' }));
  return { calls, state, save };
}

test('a copy the article could not take is moved to the trash, and the refusal is what the editor shows', async () => {
  const { calls, state, save } = annotate(() => json(200, { results: [{ id: copyId, status: 'trashed' }], batch_id: null }));
  await expect(save).rejects.toThrow(notInArticle);
  expect(calls.filter((call) => call.path === '/api/v1/kb/media/trash').map((call) => call.body)).toEqual([{ ids: [copyId] }]);
  expect(state.reload).not.toHaveBeenCalled();
  expect(state.notice).not.toHaveBeenCalled();
});

test('when the copy cannot be moved to the trash either, the editor still shows why the article did not take it', async () => {
  const { save } = annotate(() => json(500, { error: { code: 'internal', message: 'Something went wrong.' } }));
  await expect(save).rejects.toThrow(notInArticle);
});
