import { useState } from 'react';
import type { FormEvent } from 'react';
import { SignupError } from '../../api/cloud-signup';
import type { SignupAnswers } from '../../api/cloud-signup';
import { messages } from '../../messages';
import { signupMessages } from './signup-messages';

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
type Problem = keyof typeof signupMessages.signupErrors | 'unanswered';
// The answers offered are the words of each listed value; a value missing from the copy fails to compile.
const answerLabels: { [Q in Question]: Record<SignupAnswers[Q], string> } = {
  goal: signupMessages.signupQuestions.goal.options, role: signupMessages.signupQuestions.role.options, source: signupMessages.signupQuestions.source.options,
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
  // The value is a key of answerLabels[question], so it is a listed answer of that question.
  const answer = (question: Question) => (value: string) =>
    setAnswers((current) => ({ ...current, [question]: value || undefined }) as Partial<SignupAnswers>);
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
      <h2>{signupMessages.signupHeading}</h2>
      <p className="muted-copy">{buying ? signupMessages.signupBuyingExplanation : signupMessages.signupExplanation}</p>
      <label className="field"><span>{messages.workspaceName}</span>
        <input value={name} autoComplete="organization" maxLength={nameLength.max * 2} onChange={(event) => setName(event.target.value)} /></label>
      <QuestionField question="goal" value={answers.goal} onChange={answer('goal')} />
      <div className="signup-pickers">
        <PickerField question="role" value={answers.role} onChange={answer('role')} />
        <PickerField question="source" value={answers.source} onChange={answer('source')} />
      </div>
      {problem && <p role="alert" className="error-copy">{problem === 'unanswered' ? signupMessages.signupAnswerEach : signupMessages.signupErrors[problem]}</p>}
      {problem === 'trial_already_used' && <button type="button" onClick={onSignIn}>{signupMessages.signInToWorkspace}</button>}
      <button type="submit" className="primary-action" disabled={pending}>{pending ? signupMessages.creatingWorkspace : buying ? signupMessages.createWorkspaceAndPay : signupMessages.startTrial}</button>
      <button type="button" className="link-button" onClick={onUseAnotherAccount}>{signupMessages.useAnotherAccount}</button>
    </form>
  );
}

interface QuestionFieldProps {
  question: Question;
  value?: string;
  onChange: (value: string) => void;
}

// QuestionField is the goal: a radio group, so every answer is visible and arrow keys move between them.
function QuestionField({ question, value, onChange }: QuestionFieldProps) {
  return (
    <fieldset className="signup-question">
      <legend>{signupMessages.signupQuestions[question].legend}</legend>
      {Object.entries(answerLabels[question]).map(([option, label]) => (
        <label key={option} className="check-row"><input type="radio" name={`signup-${question}`} required checked={value === option}
          onChange={() => onChange(option)} /><span>{label}</span></label>))}
    </fieldset>
  );
}

// PickerField is a short question as a drop-down, so the form and its button fit a laptop screen. It
// starts on an empty prompt: nobody is counted as an answer they did not pick.
function PickerField({ question, value, onChange }: QuestionFieldProps) {
  return (
    <label className="field"><span>{signupMessages.signupQuestions[question].legend}</span>
      <select required value={value ?? ''} onChange={(event) => onChange(event.target.value)}>
        <option value="">{signupMessages.signupChooseOne}</option>
        {Object.entries(answerLabels[question]).map(([option, label]) => <option key={option} value={option}>{label}</option>)}
      </select></label>
  );
}
