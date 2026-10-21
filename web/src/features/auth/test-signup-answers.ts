import { fireEvent, screen, within } from '@testing-library/react';

// answerSignupQuestions answers each sign-up question by the words a person reads: the goal is a
// radio group, role and source are drop-downs.
export function answerSignupQuestions(answers: { goal?: string; role?: string; source?: string } = {}) {
  const { goal = 'Move off Freshdesk or another helpdesk', role = 'Founder or owner', source = 'X (Twitter)' } = answers;
  fireEvent.click(within(screen.getByRole('group', { name: 'What do you want Colliedesk to do for you first?' })).getByRole('radio', { name: goal }));
  chooseSignupAnswer('What is your role?', role);
  chooseSignupAnswer('Where did you hear about Colliedesk?', source);
}

// chooseSignupAnswer picks the option with these words in the drop-down with this label.
export function chooseSignupAnswer(label: string, words: string) {
  const select = screen.getByRole('combobox', { name: label });
  const option = within(select).getByRole('option', { name: words }) as HTMLOptionElement;
  fireEvent.change(select, { target: { value: option.value } });
}
