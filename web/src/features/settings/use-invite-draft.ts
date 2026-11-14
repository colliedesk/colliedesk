import { useState } from 'react';

// The invite being typed: the email and the roles picked for it (null until a person picks some, which means the
// Agent role). It lives where the page does, not in the tab that shows the form, so opening People and coming back
// finds it as it was left.
export interface InviteDraft {
  email: string;
  setEmail: (email: string) => void;
  picked: string[] | null;
  setPicked: (roleIds: string[] | null) => void;
}

export function useInviteDraft(): InviteDraft {
  const [email, setEmail] = useState('');
  const [picked, setPicked] = useState<string[] | null>(null);
  return { email, setEmail, picked, setPicked };
}
