import type { components } from '../api/generated';

// Message codes (docs/internals/operator-console.md › Message codes): beside the English it writes, the server
// sends a code and params for what the console shows of it (a setting's error, a live test, what a change does,
// a refused request). English shows the server's text as it is: its table, codes.en.json, is written from the
// server's templates (TestTheConsoleTableOfCodesIsTheServersEnglish) and only holds the other languages to it.
// Another language's table loads with that language (vi/index.ts) and rewrites the answers as they arrive
// (lazyWords.coded, called by api.ts), so the console's first bundle carries none of this.
export type Code = components['schemas']['OperatorMessageCode'];
export type Params = components['schemas']['OperatorMessageParams'];
// A translation of every code: a template with the same {placeholders} as the English.
export type CodeTable = Record<Code, string>;

// render fills a template as the server does (configerr.Render): {name}, {"name"} quoted, {name?one|many}.
export function render(template: string, params: Params = {}): string {
  return template.replace(/\{("?)([a-z_]+)"?(?:\?([^|}]*)\|([^}]*))?\}/g, (_, quote: string, name: string, one?: string, many?: string) => {
    const value = params[name];
    if (one !== undefined) return value === 1 ? one : (many ?? '');
    const text = value === undefined ? '' : String(value);
    return quote ? JSON.stringify(text) : text;
  });
}

type Coded = { code?: string; params?: Params };
type Body = Record<string, any>; // eslint-disable-line @typescript-eslint/no-explicit-any

// translate rewrites an API answer's texts that have a code in table, in place; a text without one stays.
export function translate(body: Body, table: Partial<Record<string, string>>) {
  const say = (code?: string, params?: Params) => (code && table[code] !== undefined ? render(table[code]!, params) : undefined);
  const texts = (target?: Record<string, string>, codes?: Record<string, Coded>) => {
    for (const [key, coded] of Object.entries(codes ?? {})) {
      const text = say(coded.code, coded.params);
      if (target && key in target && text) target[key] = text;
    }
  };
  const guards = (list?: Body[]) => {
    for (const guard of list ?? []) {
      guard.preview = say(guard.preview_code, guard.preview_params) ?? guard.preview;
      guard.refused = say(guard.refused_code, guard.refused_params) ?? guard.refused;
    }
  };
  texts(body.errors, body.field_errors);
  if (body.test) body.test.message = say(body.test.code, body.test.params) ?? body.test.message;
  guards(body.guards);
  guards(body.request?.guards);
  for (const item of Array.isArray(body.items) ? body.items : []) guards(item?.guards);
  const error = body.error as Body | undefined;
  if (error) {
    texts(error.fields, error.field_errors);
    error.message = say(error.message_code, error.params) ?? error.message;
    guards(error.guards);
  }
}
