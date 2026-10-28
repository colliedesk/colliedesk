import { useRef, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import type { MutateOptions, UseMutationOptions, UseMutationResult } from '@tanstack/react-query';
import { newSubmitKey } from './submit-key';

// Every create the web app sends goes through this module (create-key-gate.test.ts holds every other file to
// it): one press is one request, and the request names the press with an Idempotency-Key, so the server
// creates one row however many times the press arrives (a double click, a retry after a lost answer).

// A file is the same input when its name, size and date are: its bytes are not read to name a press.
const sameInput = (input: unknown) => JSON.stringify(input ?? null, (_field, value: unknown) =>
  (value instanceof Blob ? { name: (value as File).name, size: value.size, modified: (value as File).lastModified } : value));

// AnsweredWithoutSecret is thrown by a create whose repeat came back without the secret it showed once (an
// invite link, a token): what it made exists, so the next press is a new one, under a new key.
export class AnsweredWithoutSecret extends Error {
  constructor() { super('The server answered a repeat of this press without the secret it shows once.'); }
}

// Presses of one input while it is in flight share its promise.
function useOnePerInput<Input, Rest extends unknown[], Made>(send: (input: Input, ...rest: Rest) => Promise<Made>) {
  const inFlight = useRef(new Map<string, Promise<Made>>());
  return (input: Input, ...rest: Rest): Promise<Made> => {
    const name = sameInput(input);
    const running = inFlight.current.get(name);
    if (running) return running;
    const sent = send(input, ...rest).finally(() => { inFlight.current.delete(name); });
    inFlight.current.set(name, sent);
    return sent;
  };
}

export interface CreateAction<Input, Made> {
  // run sends the create, or answers the one of the same input already in flight: a second press never sends
  // a second request.
  run: (input: Input) => Promise<Made>;
  // pending is true while a create is in flight; the control that sends it is disabled meanwhile.
  pending: boolean;
}

// useCreateAction runs create with the key of its input: the same input sent again after a failure goes
// under the same key (the server answers what the first one made), new input under a new key.
export function useCreateAction<Input, Made>(create: (input: Input, key: string) => Promise<Made>): CreateAction<Input, Made> {
  const keys = useRef(new Map<string, string>());
  const [running, setRunning] = useState(0);
  const run = useOnePerInput((input: Input) => {
    const name = sameInput(input);
    const key = keys.current.get(name) ?? newSubmitKey();
    keys.current.set(name, key);
    setRunning((count) => count + 1);
    return create(input, key).then((made) => { keys.current.delete(name); return made; }, (error: unknown) => {
      if (error instanceof AnsweredWithoutSecret) keys.current.delete(name);
      throw error;
    }).finally(() => setRunning((count) => count - 1));
  });
  return { run, pending: running > 0 };
}

// useCreateMutation is useMutation for a create: mutationFn gets the press's key, and mutate or mutateAsync
// of an input already in flight sends nothing (mutateAsync answers the one in flight).
export function useCreateMutation<Made, Input = void>(
  options: Omit<UseMutationOptions<Made, Error, Input>, 'mutationFn'> & { create: (input: Input, key: string) => Promise<Made> },
): UseMutationResult<Made, Error, Input> {
  const { create, ...rest } = options;
  const action = useCreateAction(create);
  const mutation = useMutation<Made, Error, Input>({ ...rest, mutationFn: action.run });
  const { mutateAsync: send } = mutation;
  const once = useOnePerInput((input: Input, callbacks?: MutateOptions<Made, Error, Input>) => send(input, callbacks));
  // The casts only restore the optional input of a create that takes none (Input = void).
  const mutateAsync = once as UseMutationResult<Made, Error, Input>['mutateAsync'];
  const mutate = ((input: Input, callbacks?: MutateOptions<Made, Error, Input>) => {
    once(input, callbacks).catch(() => undefined);
  }) as UseMutationResult<Made, Error, Input>['mutate'];
  return { ...mutation, mutate, mutateAsync };
}
