import { useState } from 'react';
import type { FormEvent } from 'react';
import { SignupError } from '../../api/cloud-signup';
import { messages } from '../../messages';

interface CloudSignupProps {
  // onCreate rejects with a SignupError that says why the server refused.
  onCreate: (workspaceName: string) => Promise<void>;
  // onSignIn leaves sign-up for the sign-in of an existing workspace.
  onSignIn: () => void;
  onUseAnotherAccount: () => void;
  // buying is true when the visitor came to buy a plan: payment follows, not a free trial.
  buying?: boolean;
}

const nameLength = { min: 2, max: 60 };

// CloudSignup is shown when the identity provider vouched for an email that has no workspace yet.
export function CloudSignup({ onCreate, onSignIn, onUseAnotherAccount, buying = false }: CloudSignupProps) {
  const [name, setName] = useState('');
  const [pending, setPending] = useState(false);
  const [problem, setProblem] = useState<keyof typeof messages.signupErrors>();
  const submit = (event: FormEvent) => {
    event.preventDefault();
    const trimmed = name.trim();
    if (trimmed.length < nameLength.min || trimmed.length > nameLength.max) { setProblem('invalid'); return; }
    setPending(true);
    setProblem(undefined);
    onCreate(trimmed).catch((error: unknown) => setProblem(error instanceof SignupError ? error.reason : 'unavailable')).finally(() => setPending(false));
  };
  return (
    <form className="cloud-signup" onSubmit={submit} noValidate>
      <h2>{messages.signupHeading}</h2>
      <p className="muted-copy">{buying ? messages.signupBuyingExplanation : messages.signupExplanation}</p>
      <label className="field"><span>{messages.workspaceName}</span>
        <input value={name} autoComplete="organization" maxLength={nameLength.max * 2} onChange={(event) => setName(event.target.value)} /></label>
      {problem && <p role="alert" className="error-copy">{messages.signupErrors[problem]}</p>}
      {problem === 'trial_already_used' && <button type="button" onClick={onSignIn}>{messages.signInToWorkspace}</button>}
      <button type="submit" className="primary-action" disabled={pending}>{pending ? messages.creatingWorkspace : buying ? messages.createWorkspaceAndPay : messages.startTrial}</button>
      <button type="button" className="link-button" onClick={onUseAnotherAccount}>{messages.useAnotherAccount}</button>
    </form>
  );
}
