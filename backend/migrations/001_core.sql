CREATE EXTENSION IF NOT EXISTS pgcrypto;
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='qianben_ledger_owner') THEN CREATE ROLE qianben_ledger_owner NOLOGIN; END IF;
 IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='qianben_app') THEN CREATE ROLE qianben_app LOGIN PASSWORD 'dev-qianben-app' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT; END IF;
END $$;
CREATE SCHEMA IF NOT EXISTS qb;
CREATE TABLE qb.users(id uuid PRIMARY KEY, name text NOT NULL, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE qb.tokens(id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES qb.users(id), token_hash text NOT NULL UNIQUE, revoked boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE qb.ledgers(id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES qb.users(id), name text NOT NULL, currency text NOT NULL DEFAULT 'CNY' CHECK(currency='CNY'), cutover_time timestamptz NOT NULL, version bigint NOT NULL DEFAULT 0, status text NOT NULL DEFAULT 'ACTIVE' CHECK(status IN ('ACTIVE','DELETING')), created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE qb.accounts(id uuid PRIMARY KEY, ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE, code text NOT NULL, name text NOT NULL, type text NOT NULL CHECK(type IN ('ASSET','LIABILITY','EXPENSE','INCOME','EQUITY')), cash boolean NOT NULL DEFAULT false, initialized boolean NOT NULL DEFAULT false, revision bigint NOT NULL DEFAULT 0, provider text NOT NULL DEFAULT '', masked_ref text NOT NULL DEFAULT '', system boolean NOT NULL DEFAULT false, UNIQUE(ledger_id,id),UNIQUE(ledger_id,code));
CREATE TABLE qb.external_accounts(id uuid PRIMARY KEY,ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,ledger_account_id uuid NOT NULL,provider text NOT NULL,masked_ref text NOT NULL, currency text NOT NULL CHECK(currency='CNY'),FOREIGN KEY(ledger_id,ledger_account_id) REFERENCES qb.accounts(ledger_id,id),UNIQUE(ledger_id,id));
CREATE TABLE qb.payment_instruments(id uuid PRIMARY KEY,ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,external_account_id uuid NOT NULL,type text NOT NULL,FOREIGN KEY(ledger_id,external_account_id) REFERENCES qb.external_accounts(ledger_id,id));
CREATE TABLE qb.payment_rails(code text PRIMARY KEY,name text NOT NULL);
INSERT INTO qb.payment_rails VALUES('WECHAT','微信支付'),('ALIPAY','支付宝'),('UNIONPAY','银联');
CREATE TABLE qb.funding_preferences(id uuid PRIMARY KEY,ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,rail text NOT NULL REFERENCES qb.payment_rails(code),instrument_id uuid NOT NULL REFERENCES qb.payment_instruments(id),effective_from timestamptz NOT NULL, effective_to timestamptz, relation_kind text NOT NULL CHECK(relation_kind='DEFAULT_PREFERENCE'));
CREATE TABLE qb.source_events(id uuid PRIMARY KEY,ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,source_identity text NOT NULL,object_key text NOT NULL,version bigint NOT NULL DEFAULT 0,UNIQUE(ledger_id,source_identity,object_key),UNIQUE(ledger_id,id));
CREATE TABLE qb.observations(id uuid PRIMARY KEY,ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,source_event_id uuid NOT NULL,snapshot_key text NOT NULL,version bigint NOT NULL,payload_hash text NOT NULL,payload jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),FOREIGN KEY(ledger_id,source_event_id) REFERENCES qb.source_events(ledger_id,id),UNIQUE(source_event_id,snapshot_key),UNIQUE(source_event_id,version),UNIQUE(ledger_id,id));
CREATE TABLE qb.deliveries(ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,device_id text NOT NULL,delivery_id text NOT NULL,payload_hash text NOT NULL,observation_id uuid NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(ledger_id,device_id,delivery_id),FOREIGN KEY(ledger_id,observation_id) REFERENCES qb.observations(ledger_id,id));
CREATE TABLE qb.events(id uuid PRIMARY KEY,ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,status text NOT NULL DEFAULT 'REVIEW_REQUIRED' CHECK(status IN ('ACTIVE','REVIEW_REQUIRED','HISTORICAL_ONLY','MERGED','SPLIT')),revision bigint NOT NULL DEFAULT 0,current_revision_id uuid,review_reason text NOT NULL DEFAULT '',canonical_id uuid,created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(ledger_id,id));
CREATE TABLE qb.event_revisions(id uuid PRIMARY KEY,ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,event_id uuid NOT NULL,number bigint NOT NULL,facts jsonb NOT NULL,evidence_ids jsonb NOT NULL DEFAULT '[]',policy_version text NOT NULL DEFAULT 'core-v1',committed_version bigint NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),FOREIGN KEY(ledger_id,event_id) REFERENCES qb.events(ledger_id,id) DEFERRABLE INITIALLY DEFERRED,UNIQUE(event_id,number),UNIQUE(ledger_id,event_id,id));
ALTER TABLE qb.events ADD CONSTRAINT current_event_revision FOREIGN KEY(ledger_id,id,current_revision_id) REFERENCES qb.event_revisions(ledger_id,event_id,id) DEFERRABLE INITIALLY DEFERRED;
CREATE TABLE qb.evidence_links(ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,source_event_id uuid NOT NULL,event_id uuid NOT NULL,observation_id uuid NOT NULL,PRIMARY KEY(source_event_id,observation_id),FOREIGN KEY(ledger_id,source_event_id) REFERENCES qb.source_events(ledger_id,id),FOREIGN KEY(ledger_id,event_id) REFERENCES qb.events(ledger_id,id),FOREIGN KEY(ledger_id,observation_id) REFERENCES qb.observations(ledger_id,id));
CREATE TABLE qb.posting_intents(id uuid PRIMARY KEY,ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,lineage_id uuid NOT NULL,purpose text NOT NULL,current_revision_id uuid,status text NOT NULL DEFAULT 'EMPTY' CHECK(status IN ('EMPTY','ACTIVE','REVERSED')),UNIQUE(ledger_id,lineage_id,purpose),UNIQUE(ledger_id,id));
CREATE TABLE qb.posting_revisions(id uuid PRIMARY KEY,ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,intent_id uuid NOT NULL,number bigint NOT NULL,event_revision_id uuid,decision jsonb NOT NULL,committed_version bigint NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),FOREIGN KEY(ledger_id,intent_id) REFERENCES qb.posting_intents(ledger_id,id),UNIQUE(intent_id,number),UNIQUE(ledger_id,intent_id,id));
ALTER TABLE qb.posting_intents ADD CONSTRAINT current_posting_revision FOREIGN KEY(ledger_id,id,current_revision_id) REFERENCES qb.posting_revisions(ledger_id,intent_id,id) DEFERRABLE INITIALLY DEFERRED;
CREATE TABLE qb.journals(id uuid PRIMARY KEY,ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,posting_revision_id uuid NOT NULL REFERENCES qb.posting_revisions(id),lineage_id uuid NOT NULL,kind text NOT NULL CHECK(kind IN ('NORMAL','REVERSAL')),reversal_of uuid UNIQUE REFERENCES qb.journals(id) DEFERRABLE INITIALLY DEFERRED,effective_at timestamptz NOT NULL,committed_version bigint NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(posting_revision_id,kind),UNIQUE(ledger_id,id),CHECK((kind='REVERSAL')=(reversal_of IS NOT NULL)));
CREATE TABLE qb.entries(id uuid PRIMARY KEY DEFAULT gen_random_uuid(),ledger_id uuid NOT NULL,journal_id uuid NOT NULL,account_id uuid NOT NULL,debit bigint NOT NULL DEFAULT 0,credit bigint NOT NULL DEFAULT 0,CHECK(debit>=0 AND credit>=0 AND ((debit>0 AND credit=0) OR (credit>0 AND debit=0))),FOREIGN KEY(ledger_id,journal_id) REFERENCES qb.journals(ledger_id,id) ON DELETE CASCADE,FOREIGN KEY(ledger_id,account_id) REFERENCES qb.accounts(ledger_id,id));
CREATE INDEX entries_account_idx ON qb.entries(ledger_id,account_id);
CREATE TABLE qb.relations(id uuid PRIMARY KEY,ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,from_event uuid NOT NULL,to_event uuid NOT NULL,type text NOT NULL CHECK(type IN ('REFUND_OF','TRANSFER_LEG_OF','MERGED_INTO','SPLIT_FROM','REIMBURSES')),allocation bigint NOT NULL DEFAULT 0 CHECK(allocation>=0),active boolean NOT NULL DEFAULT true,committed_version bigint NOT NULL,FOREIGN KEY(ledger_id,from_event) REFERENCES qb.events(ledger_id,id),FOREIGN KEY(ledger_id,to_event) REFERENCES qb.events(ledger_id,id),CHECK(from_event<>to_event));
CREATE UNIQUE INDEX relation_active_idx ON qb.relations(ledger_id,from_event,to_event,type) WHERE active;
CREATE UNIQUE INDEX refund_origin_idx ON qb.relations(ledger_id,from_event) WHERE active AND type='REFUND_OF';
CREATE TABLE qb.relation_history(id uuid PRIMARY KEY DEFAULT gen_random_uuid(),ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,relation_id uuid NOT NULL,prior_data jsonb NOT NULL,result_data jsonb NOT NULL,committed_version bigint NOT NULL,created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE qb.assets(id uuid PRIMARY KEY,ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,event_id uuid NOT NULL,title text NOT NULL,original_minor bigint NOT NULL CHECK(original_minor>0),FOREIGN KEY(ledger_id,event_id) REFERENCES qb.events(ledger_id,id),UNIQUE(ledger_id,event_id));
CREATE TABLE qb.rules(id uuid PRIMARY KEY,ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,merchant text NOT NULL,category text NOT NULL,version bigint NOT NULL,effective_from timestamptz NOT NULL DEFAULT now(),active boolean NOT NULL DEFAULT true,created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE qb.command_receipts(ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,command_id text NOT NULL,payload_hash text NOT NULL,result jsonb NOT NULL,PRIMARY KEY(ledger_id,command_id));
CREATE TABLE qb.changes(ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,version bigint NOT NULL,command_id text NOT NULL,kind text NOT NULL,data jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(ledger_id,version));
CREATE TABLE qb.outbox(id uuid PRIMARY KEY DEFAULT gen_random_uuid(),ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,kind text NOT NULL,payload jsonb NOT NULL,dispatched boolean NOT NULL DEFAULT false,created_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX outbox_pending_idx ON qb.outbox(created_at) WHERE NOT dispatched;
CREATE TABLE qb.jobs(id uuid PRIMARY KEY,ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,kind text NOT NULL,payload jsonb NOT NULL,status text NOT NULL DEFAULT 'READY' CHECK(status IN ('READY','RUNNING','DONE','DEAD')),attempts int NOT NULL DEFAULT 0,available_at timestamptz NOT NULL DEFAULT now(),lease_token uuid,lease_until timestamptz,error_code text NOT NULL DEFAULT '',created_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX jobs_available_idx ON qb.jobs(status,available_at);
CREATE TABLE qb.projections(ledger_id uuid PRIMARY KEY REFERENCES qb.ledgers(id) ON DELETE CASCADE,source_version bigint NOT NULL,algorithm_version text NOT NULL,body jsonb NOT NULL,computed_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE qb.statement_imports(id uuid PRIMARY KEY,ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,account_id uuid NOT NULL,file_hash text NOT NULL,format text NOT NULL,rows_count int NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),FOREIGN KEY(ledger_id,account_id) REFERENCES qb.accounts(ledger_id,id),UNIQUE(ledger_id,account_id,file_hash));
CREATE TABLE qb.statement_lines(id uuid PRIMARY KEY,ledger_id uuid NOT NULL REFERENCES qb.ledgers(id) ON DELETE CASCADE,import_id uuid NOT NULL REFERENCES qb.statement_imports(id) ON DELETE CASCADE,source_record_id text NOT NULL,event_id uuid,raw_data jsonb NOT NULL,error_code text NOT NULL DEFAULT '',FOREIGN KEY(ledger_id,event_id) REFERENCES qb.events(ledger_id,id));
CREATE TABLE qb.deletion_log(ledger_id uuid PRIMARY KEY,user_id uuid NOT NULL,deleted_at timestamptz NOT NULL DEFAULT now());

CREATE FUNCTION qb.immutable_history() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='DELETE' AND current_setting('qianben.deleting',true)='on' THEN RETURN OLD; END IF;
 RAISE EXCEPTION 'immutable_history' USING ERRCODE='23514';
END $$;
DO $$ DECLARE t text;BEGIN FOREACH t IN ARRAY ARRAY['observations','event_revisions','posting_revisions','journals','entries','changes','relation_history'] LOOP EXECUTE format('CREATE TRIGGER immutable_history BEFORE UPDATE OR DELETE ON qb.%I FOR EACH ROW EXECUTE FUNCTION qb.immutable_history()',t);END LOOP;END $$;
CREATE FUNCTION qb.check_journal() RETURNS trigger LANGUAGE plpgsql AS $$ DECLARE j uuid; n bigint; d numeric;c numeric;original uuid; BEGIN
 IF TG_TABLE_NAME='journals' THEN j:=NEW.id; ELSE j:=NEW.journal_id; END IF;
 IF NOT EXISTS(SELECT 1 FROM qb.journals WHERE id=j) THEN RETURN NULL; END IF;
 SELECT count(*),coalesce(sum(debit),0),coalesce(sum(credit),0) INTO n,d,c FROM qb.entries WHERE journal_id=j;
 IF n<2 OR d<>c OR d<=0 THEN RAISE EXCEPTION 'unbalanced_journal' USING ERRCODE='23514'; END IF;
 SELECT reversal_of INTO original FROM qb.journals WHERE id=j;
 IF original IS NOT NULL AND EXISTS((SELECT account_id,debit,credit FROM qb.entries WHERE journal_id=j EXCEPT ALL SELECT account_id,credit,debit FROM qb.entries WHERE journal_id=original) UNION ALL (SELECT account_id,credit,debit FROM qb.entries WHERE journal_id=original EXCEPT ALL SELECT account_id,debit,credit FROM qb.entries WHERE journal_id=j)) THEN RAISE EXCEPTION 'invalid_reversal' USING ERRCODE='23514'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER journal_balance AFTER INSERT ON qb.journals DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION qb.check_journal();
CREATE CONSTRAINT TRIGGER entry_balance AFTER INSERT ON qb.entries DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION qb.check_journal();

CREATE FUNCTION qb.post(p_ledger uuid,p_lineage uuid,p_purpose text,p_event_revision uuid,p_entries jsonb,p_effective timestamptz,p_version bigint) RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,qb,pg_temp AS $$
DECLARE l qb.ledgers%ROWTYPE;i qb.posting_intents%ROWTYPE;r uuid;j uuid;old_j qb.journals%ROWTYPE;next_number bigint;line jsonb;BEGIN
 SELECT * INTO l FROM qb.ledgers WHERE id=p_ledger FOR UPDATE;
 IF NOT FOUND OR l.status<>'ACTIVE' OR l.user_id::text IS DISTINCT FROM current_setting('qianben.actor',true) THEN RAISE EXCEPTION 'ledger_not_authorized' USING ERRCODE='42501'; END IF;
 IF p_version<>l.version+1 THEN RAISE EXCEPTION 'invalid_commit_version' USING ERRCODE='23514'; END IF;
 IF p_purpose='ECONOMIC' THEN
  IF NOT EXISTS(SELECT 1 FROM qb.event_revisions WHERE id=p_event_revision AND ledger_id=p_ledger AND event_id=p_lineage) THEN RAISE EXCEPTION 'invalid_event_revision' USING ERRCODE='23514'; END IF;
  IF jsonb_array_length(p_entries)>0 AND p_effective<=l.cutover_time THEN RAISE EXCEPTION 'before_cutover' USING ERRCODE='23514'; END IF;
 ELSE
  IF p_purpose NOT LIKE 'OPENING:%' OR p_effective<>l.cutover_time THEN RAISE EXCEPTION 'invalid_opening' USING ERRCODE='23514'; END IF;
 END IF;
 INSERT INTO qb.posting_intents(id,ledger_id,lineage_id,purpose) VALUES(gen_random_uuid(),p_ledger,p_lineage,p_purpose) ON CONFLICT(ledger_id,lineage_id,purpose) DO NOTHING;
 SELECT * INTO i FROM qb.posting_intents WHERE ledger_id=p_ledger AND lineage_id=p_lineage AND purpose=p_purpose FOR UPDATE;
 SELECT coalesce(max(number),0)+1 INTO next_number FROM qb.posting_revisions WHERE intent_id=i.id;
 r:=gen_random_uuid();INSERT INTO qb.posting_revisions(id,ledger_id,intent_id,number,event_revision_id,decision,committed_version) VALUES(r,p_ledger,i.id,next_number,p_event_revision,p_entries,p_version);
 IF i.status='ACTIVE' THEN
  SELECT * INTO STRICT old_j FROM qb.journals WHERE posting_revision_id=i.current_revision_id AND kind='NORMAL';
  j:=gen_random_uuid();INSERT INTO qb.journals(id,ledger_id,posting_revision_id,lineage_id,kind,reversal_of,effective_at,committed_version) VALUES(j,p_ledger,r,p_lineage,'REVERSAL',old_j.id,old_j.effective_at,p_version);
  INSERT INTO qb.entries(ledger_id,journal_id,account_id,debit,credit) SELECT ledger_id,j,account_id,credit,debit FROM qb.entries WHERE journal_id=old_j.id;
 END IF;
 j:=NULL;
 IF jsonb_array_length(p_entries)>0 THEN
  j:=gen_random_uuid();INSERT INTO qb.journals(id,ledger_id,posting_revision_id,lineage_id,kind,effective_at,committed_version) VALUES(j,p_ledger,r,p_lineage,'NORMAL',p_effective,p_version);
  FOR line IN SELECT * FROM jsonb_array_elements(p_entries) LOOP
   INSERT INTO qb.entries(ledger_id,journal_id,account_id,debit,credit) VALUES(p_ledger,j,(line->>'account_id')::uuid,(line->>'debit_minor')::bigint,(line->>'credit_minor')::bigint);
  END LOOP;
 END IF;
 UPDATE qb.posting_intents SET current_revision_id=r,status=CASE WHEN j IS NULL THEN 'REVERSED' ELSE 'ACTIVE' END WHERE id=i.id;
 RETURN j;
END $$;

CREATE FUNCTION qb.purge_ledger(p_ledger uuid) RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,qb,pg_temp AS $$ DECLARE l qb.ledgers%ROWTYPE;BEGIN
 SELECT * INTO l FROM qb.ledgers WHERE id=p_ledger FOR UPDATE;
 IF NOT FOUND OR l.user_id::text IS DISTINCT FROM current_setting('qianben.actor',true) THEN RAISE EXCEPTION 'ledger_not_authorized' USING ERRCODE='42501'; END IF;
 UPDATE qb.ledgers SET status='DELETING' WHERE id=p_ledger;
 INSERT INTO qb.deletion_log(ledger_id,user_id) VALUES(l.id,l.user_id) ON CONFLICT DO NOTHING;
 PERFORM set_config('qianben.deleting','on',true);
 SET CONSTRAINTS ALL DEFERRED;
 DELETE FROM qb.ledgers WHERE id=p_ledger;
END $$;

DO $$ DECLARE t record;BEGIN
 FOR t IN SELECT tablename FROM pg_tables WHERE schemaname='qb' LOOP EXECUTE format('ALTER TABLE qb.%I OWNER TO qianben_ledger_owner',t.tablename);END LOOP;
END $$;
ALTER SCHEMA qb OWNER TO qianben_ledger_owner;
ALTER FUNCTION qb.post(uuid,uuid,text,uuid,jsonb,timestamptz,bigint) OWNER TO qianben_ledger_owner;
ALTER FUNCTION qb.purge_ledger(uuid) OWNER TO qianben_ledger_owner;
REVOKE ALL ON SCHEMA qb FROM PUBLIC;
GRANT USAGE ON SCHEMA qb TO qianben_app;
GRANT SELECT ON ALL TABLES IN SCHEMA qb TO qianben_app;
GRANT INSERT,UPDATE,DELETE ON qb.ledgers,qb.accounts,qb.external_accounts,qb.payment_instruments,qb.funding_preferences,qb.source_events,qb.deliveries,qb.events,qb.evidence_links,qb.relations,qb.assets,qb.rules,qb.command_receipts,qb.outbox,qb.jobs,qb.projections,qb.statement_imports,qb.statement_lines TO qianben_app;
GRANT INSERT ON qb.observations,qb.event_revisions,qb.changes,qb.relation_history TO qianben_app;
REVOKE DELETE ON qb.ledgers FROM qianben_app;
REVOKE ALL ON FUNCTION qb.post(uuid,uuid,text,uuid,jsonb,timestamptz,bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION qb.purge_ledger(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION qb.post(uuid,uuid,text,uuid,jsonb,timestamptz,bigint),qb.purge_ledger(uuid) TO qianben_app;
