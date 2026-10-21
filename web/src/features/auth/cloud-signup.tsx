import { useState } from 'react';
import type { FormEvent } from 'react';
import { SignupError } from '../../api/cloud-signup';
import type { SignupAnswers } from '../../api/cloud-signup';
import { messages } from '../../messages';

interface CloudSignupProps {
  // onCreate rejects with a SignupError that says why the server refused.
  onCreate: (workspaceName: string, answers: SignupAnswers) => Promise<void>;
  // onSignIn leaves sign-up for the sign-in of an existing workspace.
  onSignIn: () => void;
  onUseAnotherAccount: () => void;
  // buying is true when the visitor came to buy a plan: payment follows, not a free trial.
  buying?: boolean;
}

const nameLength = { min: 2, max: 60 };
const questions = ['goal', 'role', 'source'] as const;
type Question = typeof questions[number];
type Problem = keyof typeof messages.signupErrors | 'unanswered';
// The answers offered are the words of each listed value; a value missing from the copy fails to compile.
const answerLabels: { [Q in Question]: Record<SignupAnswers[Q], string> } = {
  goal: messages.signupQuestions.goal.options, role: messages.signupQuestions.role.options, source: messages.signupQuestions.source.options,
};

function complete(answers: Partial<SignupAnswers>): answers is SignupAnswers {
  return questions.every((question) => answers[question] !== undefined);
}

// CloudSignup is shown when the identity provider vouched for an email that has no workspace yet.
export function CloudSignup({ onCreate, onSignIn, onUseAnotherAccount, buying = false }: CloudSignupProps) {
  const [name, setName] = useState('');
  const [answers, setAnswers] = useState<Partial<SignupAnswers>>({});
  const [pending, setPending] = useState(false);
  const [problem, setProblem] = useState<Problem>();
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const trimmed = name.trim();
    if (trimmed.length < nameLength.min || trimmed.length > nameLength.max) { setProblem('invalid'); return; }
    if (!complete(answers)) { setProblem('unanswered'); return; }
    setPending(true);
    setProblem(undefined);
    onCreate(trimmed, answers).catch((error: unknown) => setProblem(error instanceof SignupError ? error.reason : 'unavailable')).finally(() => setPending(false));
  };
  return (
    <form className="cloud-signup" onSubmit={submit} noValidate>
      <h2>{messages.signupHeading}</h2>
      <p className="muted-copy">{buying ? messages.signupBuyingExplanation : messages.signupExplanation}</p>
      <label className="field"><span>{messages.workspaceName}</span>
        <input value={name} autoComplete="organization" maxLength={nameLength.max * 2} onChange={(event) => setName(event.target.value)} /></label>
      {questions.map((question) => <QuestionField key={question} question={question} value={answers[question]}
        // The value is a key of answerLabels[question], so it is a listed answer of that question.
        onChange={(value) => setAnswers((current) => ({ ...current, [question]: value }) as Partial<SignupAnswers>)} />)}
      {problem && <p role="alert" className="error-copy">{problem === 'unanswered' ? messages.signupAnswerEach : messages.signupErrors[problem]}</p>}
      {problem === 'trial_already_used' && <button type="button" onClick={onSignIn}>{messages.signInToWorkspace}</button>}
      <button type="submit" className="primary-action" disabled={pending}>{pending ? messages.creatingWorkspace : buying ? messages.createWorkspaceAndPay : messages.startTrial}</button>
      <button type="button" className="link-button" onClick={onUseAnotherAccount}>{messages.useAnotherAccount}</button>
    </form>
  );
}

interface QuestionFieldProps {
  question: Question;
  value?: string;
  onChange: (value: string) => void;
}

// QuestionField is one single-choice question: a radio group, so arrow keys move between its answers.
function QuestionField({ question, value, onChange }: QuestionFieldProps) {
  return (
    <fieldset className="signup-question" data-short={question !== 'goal' || undefined}>
      <legend>{messages.signupQuestions[question].legend}</legend>
      {Object.entries(answerLabels[question]).map(([option, label]) => (
        <label key={option} className="check-row"><input type="radio" name={`signup-${question}`} required checked={value === option}
          onChange={() => onChange(option)} /><span>{label}</span></label>))}
    </fieldset>
  );
}
