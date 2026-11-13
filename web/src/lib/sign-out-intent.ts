// A person's own Sign out (the account menu, or this device in Sessions), or a session someone revoked (401
// session_revoked), is remembered until a session answers again. Only then does the sign-in page end the identity
// provider's session too, so the next sign-in asks for a code. A session that expired, or none yet (APP_ORIGIN),
// keeps it, and the person signs straight back in or chooses a workspace without a new code.
let signedOut = false;

export const rememberSignOut = () => { signedOut = true; };
export const forgetSignOut = () => { signedOut = false; };
export const signedOutByPerson = () => signedOut;
