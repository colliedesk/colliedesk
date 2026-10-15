const units = ['KB', 'MB', 'GB'];

// formatBytes writes a size in binary units. Usage is written with 'down' so a workspace under its
// limit never reads as at it ("2.0 GB of 2.0 GB" while 2147000000 bytes are used).
export function formatBytes(size: number, rounding: 'nearest' | 'down' = 'nearest'): string {
  if (size < 1024) return `${size} B`;
  let value = size / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const shown = rounding === 'down' ? (Math.floor(value * 10 + 1e-9) / 10).toFixed(1) : value.toFixed(1);
  return `${shown} ${units[unit]}`;
}
