import { createServer } from 'node:http';
import type { Server } from 'node:http';
import { ports } from './checkout';

interface ChatRequest {
  model: string;
  messages: { role: string; content: string }[];
  response_format?: { json_schema?: { schema?: { properties?: Record<string, unknown> } } };
}

// FakeAI answers the OpenAI-compatible chat completions API the way a model would for an AI draft:
// the article chooser gets no articles (the knowledge base is sent whole), a draft cites the first
// source. It keeps every prompt, so a test can check what left Colliedesk.
export class FakeAI {
  readonly prompts: string[] = [];
  private server?: Server;
  constructor(private readonly reply: string) {}

  async start(): Promise<void> {
    this.server = createServer((request, response) => {
      let body = '';
      request.on('data', (chunk: Buffer) => { body += chunk.toString(); });
      request.on('end', () => {
        const chat = JSON.parse(body) as ChatRequest;
        this.prompts.push(chat.messages.map((message) => message.content).join('\n'));
        const choosing = Boolean(chat.response_format?.json_schema?.schema?.properties?.articles);
        const content = JSON.stringify(choosing ? { articles: [] } : {
          action: 'answer', reply: this.reply, claims: [{ text: this.reply, source: 'S1' }], confidence: 'high', risks: [], handoff_reason: '',
          triage: { tags: [], priority: 'normal', reason: 'How-to question' },
        });
        response.writeHead(200, { 'Content-Type': 'application/json' });
        response.end(JSON.stringify({ id: 'fake', model: chat.model, choices: [{ index: 0, message: { role: 'assistant', content }, finish_reason: 'stop' }],
          usage: { prompt_tokens: 100, completion_tokens: 20 } }));
      });
    });
    await new Promise<void>((resolve) => this.server!.listen(ports.ai, '127.0.0.1', resolve));
  }

  async stop(): Promise<void> {
    this.server?.closeAllConnections();
    await new Promise<void>((resolve) => (this.server ? this.server.close(() => resolve()) : resolve()));
  }
}

export const fakeAI = {
  env(): Record<string, string> {
    return { AI_SERVER_PROVIDER: 'openai_compatible', AI_SERVER_BASE_URL: `http://127.0.0.1:${ports.ai}/v1`, AI_SERVER_MODEL: 'stand-in', AI_SERVER_SELECT_MODEL: 'stand-in-chooser' };
  },
};
