package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func (d *DB) Migrate(ctx context.Context, dir string) error {
	tx, e := d.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(819201)`); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS public.qianben_migrations(name text PRIMARY KEY,sha256 text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); e != nil {
		return e
	}
	paths, e := filepath.Glob(filepath.Join(dir, "*.sql"))
	if e != nil {
		return e
	}
	sort.Strings(paths)
	for _, path := range paths {
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		name := filepath.Base(path)
		var exists bool
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.qianben_migrations WHERE name=$1)`, name).Scan(&exists)
		if e != nil {
			return e
		}
		if exists {
			var old string
			e = tx.QueryRow(ctx, `SELECT sha256 FROM public.qianben_migrations WHERE name=$1`, name).Scan(&old)
			if e != nil {
				return e
			}
			if old != Hash(b) {
				return fmt.Errorf("migration %s checksum changed", name)
			}
			continue
		}
		var legacy bool
		if name == "001_core.sql" {
			e = tx.QueryRow(ctx, `SELECT to_regclass('qb.ledgers') IS NOT NULL`).Scan(&legacy)
			if e != nil {
				return e
			}
		}
		if !legacy {
			if _, e = tx.Exec(ctx, string(b)); e != nil {
				return fmt.Errorf("migration %s: %w", name, e)
			}
		}
		if _, e = tx.Exec(ctx, `INSERT INTO public.qianben_migrations(name,sha256) VALUES($1,$2)`, name, Hash(b)); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
