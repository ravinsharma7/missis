-- Persist core-assigned containment order in the rebuildable current
-- projection. Empty keys retain the pre-order-key stream-sequence/part-id fallback.
ALTER TABLE parts_current ADD COLUMN order_key TEXT NOT NULL DEFAULT '';
