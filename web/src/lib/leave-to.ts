// leaveTo sends the browser to another site: the workspace's own host after a sign-in on APP_ORIGIN.
export function leaveTo(url: string) {
  window.location.assign(url);
}
