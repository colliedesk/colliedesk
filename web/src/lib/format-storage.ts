const MiB = 1024 * 1024;
const GiB = MiB * 1024;

function trim(value: number): string {
  return String(Math.round(value * 10) / 10);
}

// formatStorage writes a storage size the way the plans name their limits: "5 GiB", "17.5 GiB",
// "300 MiB". formatBytes (decimal-looking "KB/MB/GB") is for file sizes in the timeline.
export function formatStorage(bytes: number): string {
  if (bytes >= GiB) return `${trim(bytes / GiB)} GiB`;
  if (bytes >= MiB) return `${trim(bytes / MiB)} MiB`;
  return `${Math.round(bytes / 1024)} KiB`;
}
