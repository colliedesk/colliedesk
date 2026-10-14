// Waits until a URL answers. The extra test servers start after the main one has migrated the
// shared database: serve refuses an unmigrated database, and two migrations must not race.
const [url, timeoutMs = '120000'] = process.argv.slice(2);
const deadline = Date.now() + Number(timeoutMs);
while (Date.now() < deadline) {
  try {
    if ((await fetch(url)).ok) process.exit(0);
  } catch { /* not up yet */ }
  await new Promise((resolve) => setTimeout(resolve, 250));
}
console.error(`${url} did not answer within ${timeoutMs} ms`);
process.exit(1);
