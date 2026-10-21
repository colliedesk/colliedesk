import { fireEvent, screen, within } from '@testing-library/react';

// answerSignupQuestions picks one answer in each sign-up question, by the words a person reads.
export function answerSignupQuestions(answers: { goal?: string; role?: string; source?: string } = {}) {
  const { goal = 'Move off Freshdesk or another helpdesk', role = 'Founder or owner', source = 'X (Twitter)' } = answers;
  fireEvent.click(within(screen.getByRole('group', { name: 'What do you want Colliedesk to do for you first?' })).getByRole('radio', { name: goal }));
  fireEvent.click(within(screen.getByRole('group', { name: 'What is your role?' })).getByRole('radio', { name: role }));
  fireEvent.click(within(screen.getByRole('group', { name: 'Where did you hear about Colliedesk?' })).getByRole('radio', { name: source }));
}
