import { createContext, useContext } from 'react';

// PermissionsContext holds the session's permissions for the parts of a page that offer an action or show
// data only some roles reach. Without a provider (a part rendered alone) everything is offered, as before.
export const PermissionsContext = createContext<readonly string[] | undefined>(undefined);

export function useHolds(): (permission: string) => boolean {
  const held = useContext(PermissionsContext);
  return (permission) => held === undefined || held.includes(permission);
}
