-- Messages can carry videos as well as images, so the list of attached files
-- in a message body is named "media" instead of "images".
UPDATE entries
SET body = json_remove(json_set(body, '$.media', json(json_extract(body, '$.images'))), '$.images')
WHERE json_type(body, '$.images') = 'array';
