-- The browser ID only sharpens a report to Meta; reports still go out with the click, address and browser.
ALTER TABLE meta_conversion_events DROP COLUMN fbp;
