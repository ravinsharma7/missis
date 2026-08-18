-- Derived current projection tables (ticket #51). The event ledger remains
-- authoritative; these tables are a rebuildable current-time snapshot,
-- maintained transactionally on append.

CREATE TABLE tickets (
  ticket_id   TEXT PRIMARY KEY,
  alias       INTEGER NOT NULL,
  title       TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL DEFAULT '',
  head_event  TEXT,
  recorded_at TEXT
);

CREATE INDEX idx_tickets_alias ON tickets(alias);

CREATE TABLE parts_current (
  ticket_id     TEXT NOT NULL,
  path          TEXT NOT NULL,
  part_id       TEXT NOT NULL,
  value_json    TEXT,
  value_kind    TEXT NOT NULL,
  parent_id     TEXT,
  created_by    TEXT,
  current_event TEXT,
  PRIMARY KEY (ticket_id, path)
);

CREATE INDEX idx_parts_current_ticket ON parts_current(ticket_id);
