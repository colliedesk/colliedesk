// A tab opened before a deploy still runs the previous build. Its chunk names are gone from the server (they are
// named by content hash and the server only has the current build), so the first page part it has not loaded yet
// fails with 404. Loading the app again fetches the current build, and the part works. This module does that once:
// a mark in session storage says when this tab last reloaded for a chunk, so a chunk that still fails right after
// (a network fault, not a deploy) is shown with PartUnavailable instead of reloading in a loop.
//
// A page about to leave with unsaved text still asks first: navigation.assign fires the browser's beforeunload,
// which the editors' useBlocker turns into the "Leave site?" prompt.

export const reloadMarkKey = 'colliedesk:reloaded-for-new-build';
// A reload newer than this is the one that should have fixed the chunk; an older one was for an earlier deploy.
const loopWindowMs = 60_000;
// The mark this page wrote: a page that finds its own mark has a reload under way already.
let ownMark: string | undefined;

// navigation is the browser boundary: tests replace assign, the app never does.
export const navigation = { assign: (href: string) => window.location.assign(href) };

// reloadForNewBuild loads href as a full page, unless this tab did so less than a minute ago or session storage
// cannot hold the mark (the time of the reload). It says whether a reload is under way: then the caller keeps its
// placeholder instead of an error, as the page is about to be replaced.
export function reloadForNewBuild(href: string): boolean {
  try {
    const mark = sessionStorage.getItem(reloadMarkKey);
    if (mark && mark === ownMark) return true;
    if (Date.now() - Number(mark) < loopWindowMs) return false;
    ownMark = String(Date.now());
    sessionStorage.setItem(reloadMarkKey, ownMark);
  } catch {
    return false;
  }
  navigation.assign(href);
  return true;
}
