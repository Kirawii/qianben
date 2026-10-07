CREATE INDEX events_page_idx ON qb.events(ledger_id,created_at DESC,id DESC);
