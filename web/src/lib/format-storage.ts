const MiB = 1024 * 1024;
const GiB = MiB * 1024;

type Rounding = 'nearest' | 'down';

function trim(value: number, rounding: Rounding): string {
  const tenths = value * 10;
  return String((rounding === 'down' ? Math.floor(tenths + 1e-9) : Math.round(tenths)) / 10);
}

// formatStorage writes a storage size the way the plans name their limits: "5 GiB", "17.5 GiB",
// "300 MiB". Usage is written with 'down' so a workspace under its limit never reads as at it.
// formatBytes (decimal-looking "KB/MB/GB") is for file sizes in the timeline.
export function formatStorage(bytes: number, rounding: Rounding = 'nearest'): string {
  if (bytes >= GiB) return `${trim(bytes / GiB, rounding)} GiB`;
  if (bytes >= MiB) return `${trim(bytes / MiB, rounding)} MiB`;
  return `${Math.floor(bytes / 1024)} KiB`;
}
