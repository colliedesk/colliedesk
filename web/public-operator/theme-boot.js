// Sets the saved console theme while the head is parsed, before first paint (src/operator/theme.ts keeps the same
// key and rule). An external file, not an inline script: the console's CSP allows scripts from itself only.
try {
  if (localStorage.getItem('colliedesk.operator.theme') === 'dark') {
    document.documentElement.dataset.theme = 'dark';
    document.querySelector('meta[name="theme-color"]').content = '#0a0a0a';
  }
} catch (e) {}
