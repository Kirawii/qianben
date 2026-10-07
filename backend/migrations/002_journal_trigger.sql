-- Repair the initial migration for databases created before the trigger fix.
CREATE OR REPLACE FUNCTION qb.check_journal() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE j uuid; n bigint; d numeric; c numeric; original uuid;
BEGIN
 IF TG_TABLE_NAME='journals' THEN j:=NEW.id; ELSE j:=NEW.journal_id; END IF;
 IF NOT EXISTS(SELECT 1 FROM qb.journals WHERE id=j) THEN RETURN NULL; END IF;
 SELECT count(*),coalesce(sum(debit),0),coalesce(sum(credit),0) INTO n,d,c FROM qb.entries WHERE journal_id=j;
 IF n<2 OR d<>c OR d<=0 THEN RAISE EXCEPTION 'unbalanced_journal' USING ERRCODE='23514'; END IF;
 SELECT reversal_of INTO original FROM qb.journals WHERE id=j;
 IF original IS NOT NULL AND EXISTS((SELECT account_id,debit,credit FROM qb.entries WHERE journal_id=j EXCEPT ALL SELECT account_id,credit,debit FROM qb.entries WHERE journal_id=original) UNION ALL (SELECT account_id,credit,debit FROM qb.entries WHERE journal_id=original EXCEPT ALL SELECT account_id,debit,credit FROM qb.entries WHERE journal_id=j)) THEN RAISE EXCEPTION 'invalid_reversal' USING ERRCODE='23514'; END IF;
 RETURN NULL;
END $$;
