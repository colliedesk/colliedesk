import { createContext, useContext } from 'react';

// The help center a settings section acts on. Undefined is the primary, which the singular
// /api/v1/help-center routes answer for when no help_center_id is given; a section inside the
// settings of another help center reads and saves that one's settings by its id.
export const HelpCenterTarget = createContext<string | undefined>(undefined);

export const useHelpCenterTarget = () => useContext(HelpCenterTarget);

// onHelpCenter is the query that names the help center, or nothing for the primary.
export const onHelpCenter = (id?: string) => (id ? `?help_center_id=${encodeURIComponent(id)}` : '');

// keyOf keeps the primary's cache key as it was and gives each other help center a key of its own.
export const keyOf = (key: readonly string[], id?: string): readonly string[] => (id ? [...key, id] : key);
