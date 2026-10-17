const browsers: [RegExp, string][] = [
  [/Edg(A|iOS)?\//, 'Edge'], [/OPR\//, 'Opera'], [/(Firefox|FxiOS)\//, 'Firefox'], [/(Chrome|CriOS)\//, 'Chrome'], [/Version\/[\d.]+.*Safari\//, 'Safari'],
];
const systems: [RegExp, string][] = [
  [/iPhone/, 'iPhone'], [/iPad/, 'iPad'], [/Android/, 'Android'], [/CrOS/, 'ChromeOS'], [/Windows/, 'Windows'], [/Mac OS X|Macintosh/, 'macOS'], [/Linux/, 'Linux'],
];

// describeUserAgent turns a browser's user agent into "Chrome on Linux". A label that is not a
// browser user agent (a CLI, "Cloud sign-in") returns null and is shown as it is.
export function describeUserAgent(label: string): string | null {
  if (!label.startsWith('Mozilla/')) return null;
  const browser = browsers.find(([pattern]) => pattern.test(label))?.[1];
  const system = systems.find(([pattern]) => pattern.test(label))?.[1];
  if (browser && system) return `${browser} on ${system}`;
  return browser ?? system ?? null;
}
