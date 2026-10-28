-- acme's help center has a full theme: its library image as logo and favicon, links, every color and
-- both fonts. beta keeps the default look.
UPDATE help_centers SET logo_media_id = md5('kbi-acme-1')::uuid, favicon_media_id = md5('kbi-acme-1')::uuid,
       logo_alt = 'Acme', linkback_url = 'https://acme.example/', contact_url = 'mailto:help@acme.example',
       contact_phone = '+1 (555) 010-2030', color_primary = '#0b5cad', color_header = '#ffffff',
       color_header_text = '#16201c', color_background = '#ffffff', color_text = '#1f2328', color_link = '#0b5cad',
       color_footer = '#f6f8fa', body_font = 'humanist-sans', heading_font = 'transitional-serif'
WHERE workspace_id = md5('ws-acme')::uuid;
