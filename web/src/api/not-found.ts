// isNotFound tells a 404 (an id that names nothing in this workspace: wrong, removed, another workspace's)
// from a failure that may pass. request fails a 404 as Error('API 404') (api/client.ts).
export const isNotFound = (error: unknown): boolean => error instanceof Error && error.message === 'API 404';
