-- Ledger deletion cascades cross references; validate them at commit after all
-- ledger-owned rows have been removed, rather than in cascade trigger order.
DO $$ DECLARE c record; BEGIN
 FOR c IN SELECT n.nspname,t.relname,k.conname FROM pg_constraint k JOIN pg_class t ON t.oid=k.conrelid JOIN pg_namespace n ON n.oid=t.relnamespace WHERE n.nspname='qb' AND k.contype='f' LOOP
  EXECUTE format('ALTER TABLE %I.%I ALTER CONSTRAINT %I DEFERRABLE INITIALLY DEFERRED',c.nspname,c.relname,c.conname);
 END LOOP;
END $$;
