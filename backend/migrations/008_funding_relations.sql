CREATE TABLE qb.funding_relations(
 id uuid PRIMARY KEY,
 ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,
 kind text NOT NULL CHECK(kind IN ('DEFAULT_PREFERENCE','USER_CONFIRMED','TRANSACTION_EVIDENCE')),
 scope text NOT NULL CHECK(scope IN ('RAIL_TIMELINE','EVENT_REVISION')),
 account_id uuid,
 rail text REFERENCES qb.payment_rails(code),
 effective_from timestamptz,
 effective_to timestamptz,
 event_id uuid,
 event_revision_id uuid,
 observation_id uuid,
 leg text CHECK(leg IN ('FUNDING','REPAYMENT')),
 actor_id uuid NOT NULL REFERENCES qb.users(id),
 source_version bigint NOT NULL,
 note text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(ledger_id,account_id) REFERENCES qb.accounts(ledger_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(ledger_id,event_id,event_revision_id) REFERENCES qb.event_revisions(ledger_id,event_id,id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(ledger_id,observation_id) REFERENCES qb.observations(ledger_id,id) DEFERRABLE INITIALLY DEFERRED,
 CHECK((kind='DEFAULT_PREFERENCE' AND scope='RAIL_TIMELINE' AND rail IS NOT NULL AND effective_from IS NOT NULL AND event_id IS NULL AND event_revision_id IS NULL AND observation_id IS NULL AND leg IS NULL)
 OR(kind<>'DEFAULT_PREFERENCE' AND scope='EVENT_REVISION' AND account_id IS NOT NULL AND event_id IS NOT NULL AND event_revision_id IS NOT NULL AND observation_id IS NOT NULL AND leg IS NOT NULL AND rail IS NULL AND effective_from IS NULL AND effective_to IS NULL)),
 CHECK(effective_to IS NULL OR effective_to>effective_from)
);
CREATE UNIQUE INDEX funding_timeline_point ON qb.funding_relations(ledger_id,rail,effective_from) WHERE kind='DEFAULT_PREFERENCE';
CREATE INDEX funding_event_revision ON qb.funding_relations(ledger_id,event_id,event_revision_id);
CREATE TRIGGER immutable_history BEFORE UPDATE OR DELETE ON qb.funding_relations FOR EACH ROW EXECUTE FUNCTION qb.immutable_history();
ALTER TABLE qb.funding_relations OWNER TO qianben_ledger_owner;
GRANT SELECT,INSERT ON qb.funding_relations TO qianben_app;
