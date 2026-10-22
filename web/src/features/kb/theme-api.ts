import { z } from 'zod';
import { ConflictError, InvalidRequestError, request } from '../../api/client';
import type { RequestBody } from '../../api/client';
import type { Generated, JsonResponse } from '../../api/schemas';

// The help center's brand: read, save, and a preview of a page with a theme not saved yet. These calls
// live in the help center card's chunk, so the first bundle does not carry them.

export const colorFields = ['primary', 'header', 'header_text', 'background', 'text', 'link', 'footer'] as const;
export type ColorField = typeof colorFields[number];
export const fontKeys = ['system-sans', 'humanist-sans', 'geometric-sans', 'classic-serif', 'transitional-serif', 'rounded-sans', 'mono'] as const;

const color = z.string().regex(/^#[0-9a-fA-F]{6}$/).nullable();
const font = z.enum(fontKeys).nullable();
const checkSchema = z.object({
  pair: z.enum(['text_on_background', 'link_on_background', 'header_text_on_header', 'ink_on_primary', 'footer_ink_on_footer', 'primary_on_background']),
  field: z.string(), foreground: z.string(), background: z.string(), ratio: z.number(), required: z.number(), passes: z.boolean(), suggestion: z.string().nullable(),
}) satisfies z.ZodType<Generated<'ThemeContrastCheck'>>;
const colorsSchema = z.object({ primary: color, header: color, header_text: color, background: color, text: color, link: color, footer: color });
const themeSchema = z.object({
  logo_media_id: z.string().uuid().nullable(), favicon_media_id: z.string().uuid().nullable(), logo_alt: z.string(),
  linkback_url: z.string().nullable(), contact_url: z.string().nullable(), contact_phone: z.string().nullable(),
  colors: colorsSchema, body_font: font, heading_font: font, custom_css: z.string(), content_version: z.number().int(), updated_at: z.string().datetime().nullable(),
  checks: z.array(checkSchema),
}) satisfies z.ZodType<Generated<'HelpCenterTheme'>>;
// A map of messages by field. catchall rather than z.record: the first bundle, which holds zod, then keeps
// no code for this part alone.
const messagesSchema = z.object({}).catchall(z.string());
const previewSchema = z.object({
  html: z.string(), ok: z.boolean(), checks: z.array(checkSchema), fields: messagesSchema,
}) satisfies z.ZodType<JsonResponse<'previewHelpCenterTheme', 200>>;

export type HelpCenterTheme = z.infer<typeof themeSchema>;
export type ThemeCheck = z.infer<typeof checkSchema>;
export type ThemePreview = z.infer<typeof previewSchema>;
export type ThemeInput = RequestBody<'putHelpCenterTheme'>;
export type PreviewPage = RequestBody<'previewHelpCenterTheme'>['page'];

// ThemeInvalidError is a theme the server refused (400 or 422). fields holds one message per field, keyed
// like colors.text, so each shows next to its control.
export class ThemeInvalidError extends InvalidRequestError {
  readonly fields: Record<string, string>;

  constructor(message: string, fields: Record<string, string>) {
    super(message);
    this.name = 'ThemeInvalidError';
    this.fields = fields;
  }
}

const fieldsSchema = z.object({ fields: messagesSchema });

// send is the app's request with a refusal's field messages kept: every other failure stays as request()
// makes it (a 401 signs out, a failed call is kept for Report a problem).
async function send<T>(path: string, schema: z.ZodType<T>, init?: RequestInit): Promise<T> {
  try {
    return await request(path, schema, init);
  } catch (error) {
    if (!(error instanceof InvalidRequestError) || error instanceof ConflictError) throw error;
    const detail = fieldsSchema.safeParse(error.detail);
    throw new ThemeInvalidError(error.message, detail.success ? detail.data.fields : {});
  }
}

const themePath = '/api/v1/help-center/theme';

export const themeApi = {
  get: () => send(themePath, themeSchema),
  save: (theme: ThemeInput) => send(themePath, themeSchema, { method: 'PUT', body: JSON.stringify(theme) }),
  preview: (page: PreviewPage, theme: ThemeInput) => send('/api/v1/help-center/theme/preview', previewSchema,
    { method: 'POST', body: JSON.stringify({ page, theme } satisfies RequestBody<'previewHelpCenterTheme'>) }),
};
