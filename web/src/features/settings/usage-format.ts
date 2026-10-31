const GiB = 1024 ** 3;

// gibOf is a size in GiB with one decimal and no trailing zero: "0.2", "250". It cuts down and never rounds up,
// so usage just under a limit never reads as the limit.
export const gibOf = (bytes: number): string => String(Math.floor((bytes / GiB) * 10 + 1e-9) / 10);
