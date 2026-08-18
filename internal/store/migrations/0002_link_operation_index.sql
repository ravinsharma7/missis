CREATE INDEX IF NOT EXISTS idx_events_link_operation
ON events(json_extract(event_json, '$.Operation'));
