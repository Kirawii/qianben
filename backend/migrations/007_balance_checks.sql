CREATE TABLE qb.balance_checks(
 id uuid PRIMARY KEY,
 ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,
 account_id uuid NOT NULL,
 as_of timestamptz NOT NULL,
 actual_minor bigint NOT NULL,
 book_minor bigint NOT NULL,
 difference_minor bigint NOT NULL,
 source_version bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(ledger_id,account_id) REFERENCES qb.accounts(ledger_id,id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX balance_checks_account_idx ON qb.balance_checks(ledger_id,account_id,created_at DESC);
CREATE TRIGGER immutable_history BEFORE UPDATE OR DELETE ON qb.balance_checks FOR EACH ROW EXECUTE FUNCTION qb.immutable_history();
ALTER TABLE qb.balance_checks OWNER TO qianben_ledger_owner;
GRANT SELECT,INSERT ON qb.balance_checks TO qianben_app;
