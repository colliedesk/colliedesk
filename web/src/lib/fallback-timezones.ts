// fallbackTimezones is offered only where the browser cannot list its zones (Intl.supportedValuesOf
// is missing, Safari before 15.4): one widely used IANA zone per common UTC offset and region.
export const fallbackTimezones = [
  'Pacific/Honolulu', 'America/Anchorage', 'America/Los_Angeles', 'America/Denver', 'America/Phoenix', 'America/Chicago',
  'America/Mexico_City', 'America/New_York', 'America/Toronto', 'America/Bogota', 'America/Lima', 'America/Halifax',
  'America/Santiago', 'America/Sao_Paulo', 'America/Argentina/Buenos_Aires', 'Atlantic/Azores', 'Europe/London', 'Europe/Dublin',
  'Europe/Lisbon', 'Africa/Lagos', 'Europe/Paris', 'Europe/Berlin', 'Europe/Madrid', 'Europe/Rome', 'Europe/Amsterdam',
  'Europe/Stockholm', 'Europe/Warsaw', 'Africa/Johannesburg', 'Africa/Cairo', 'Europe/Athens', 'Europe/Helsinki', 'Europe/Kyiv',
  'Europe/Istanbul', 'Europe/Moscow', 'Africa/Nairobi', 'Asia/Riyadh', 'Asia/Dubai', 'Asia/Tehran', 'Asia/Karachi',
  'Asia/Kolkata', 'Asia/Kathmandu', 'Asia/Dhaka', 'Asia/Yangon', 'Asia/Bangkok', 'Asia/Ho_Chi_Minh', 'Asia/Jakarta',
  'Asia/Singapore', 'Asia/Kuala_Lumpur', 'Asia/Manila', 'Asia/Shanghai', 'Asia/Hong_Kong', 'Asia/Taipei', 'Australia/Perth',
  'Asia/Seoul', 'Asia/Tokyo', 'Australia/Adelaide', 'Australia/Brisbane', 'Australia/Sydney', 'Pacific/Auckland',
];
