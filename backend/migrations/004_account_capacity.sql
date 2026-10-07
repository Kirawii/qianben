CREATE FUNCTION qb.check_account_capacity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE balance numeric;
BEGIN
 SELECT COALESCE(sum(debit-credit),0) INTO balance FROM qb.entries WHERE ledger_id=NEW.ledger_id AND account_id=NEW.account_id;
 IF abs(balance)>9000000000000000000 THEN RAISE EXCEPTION 'account_capacity_exceeded' USING ERRCODE='23514'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER account_capacity AFTER INSERT ON qb.entries DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION qb.check_account_capacity();
