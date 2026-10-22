import { fireEvent, render, screen } from '@testing-library/react';
import { expect, test, vi } from 'vitest';
import { AISection } from './ai-section';
import type { AIWorkspace } from './ai-settings-api';

const workspace: AIWorkspace = {
  settings: { available: true, drafts_enabled: true, auto_answer_enabled: false, auto_answer_delay_seconds: 300, auto_answer_min_reviewed: 10, auto_answer_min_accuracy: 90, updated_at: null },
  topics: [
    { article_id: '11111111-1111-4111-8111-111111111111', article_number: 12, title: 'Refund policy', reviewed: 12, accepted: 11, eligible: false },
    { article_id: '22222222-2222-4222-8222-222222222222', article_number: 13, title: 'Export invoices', reviewed: 4, accepted: 4, eligible: false },
  ],
  usage: { drafts: 40, calls: 44, input_tokens: 400000, output_tokens: 30000, sent_unchanged: 20, sent_edited: 8, discarded: 3, auto_sent: 0 },
};

test('without an AI provider the section says how the server gets one, and offers no switches', () => {
  render(<AISection workspace={{ ...workspace, settings: { ...workspace.settings, available: false } }} canManage onSave={vi.fn()} />);
  expect(screen.getByText(/AI_SERVER_PROVIDER/)).toBeInTheDocument();
  expect(screen.queryByRole('checkbox')).not.toBeInTheDocument();
});

test('an admin turns auto-answer on with its bar, and each topic shows its record', () => {
  const onSave = vi.fn();
  render(<AISection workspace={workspace} canManage onSave={onSave} />);
  expect(screen.getByRole('row', { name: /#12 Refund policy 12 11 \(92%\)/ })).toBeInTheDocument();
  expect(screen.getByText(/20 sent unchanged/)).toBeInTheDocument();
  fireEvent.click(screen.getByRole('checkbox', { name: /Answer on its own/ }));
  fireEvent.change(screen.getByLabelText('Minutes before an answer goes out'), { target: { value: '10' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save AI settings' }));
  expect(onSave).toHaveBeenCalledWith({ drafts_enabled: true, auto_answer_enabled: true, auto_answer_delay_seconds: 600, auto_answer_min_reviewed: 10, auto_answer_min_accuracy: 90 });
});

test('an agent sees the settings and the records but cannot change them', () => {
  render(<AISection workspace={workspace} canManage={false} onSave={vi.fn()} />);
  expect(screen.getByText(/Auto-answer is off/)).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Save AI settings' })).not.toBeInTheDocument();
});
