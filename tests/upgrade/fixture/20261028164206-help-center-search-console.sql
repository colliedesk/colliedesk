-- acme verified its help center host in Google Search Console and Bing Webmaster Tools; beta has no tokens.
UPDATE help_centers SET google_site_verification = 'Ab3dEf6hIj9lMn2pQr5tUv8xYz1-Ab3dEf6hIj9lMn2', bing_site_verification = '0A1B2C3D4E5F60718293A4B5C6D7E8F9'
WHERE workspace_id = md5('ws-acme')::uuid;
