package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/touchline/backend/pkg/playergen"
)

// ref-seed ingests the curated data/names/*.json files into the reference
// tables ref.nationalities and ref.name_pool. It is idempotent by
// construction: ref.nationalities rows are upserted and ref.name_pool rows are
// deleted and re-inserted for the ingested codes inside one transaction, with
// the JSON files as the sole authority.
//
// It deliberately never writes game entities (no person.people, no
// player.players). Generated players are persisted later, when a team is
// created or the first season starts — guaranteeing fresh-slate worlds.
func main() {
	ctx := context.Background()

	databaseURL := flag.String("database", "", "postgres connection URL (or $DATABASE_URL)")
	dataDir := flag.String("data", "data/names", "path to the curated name data directory")
	flag.Parse()
	if *databaseURL == "" {
		*databaseURL = os.Getenv("DATABASE_URL")
	}
	if *databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	data, err := playergen.LoadNameData(*dataDir)
	if err != nil {
		log.Fatalf("load name data: %v", err)
	}
	log.Printf("loaded %d nationalities from %s", len(data.Nationalities), *dataDir)

	pool, err := pgxpool.New(ctx, *databaseURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	codes := data.Pool.Codes()
	var names, firstCount, lastCount int

	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Fatalf("begin transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	for _, n := range data.Nationalities {
		if _, err := tx.Exec(ctx, `
			INSERT INTO ref.nationalities (code, name, generation_weight, attribute_bias)
			VALUES ($1, $2, $3, '{}'::jsonb)
			ON CONFLICT (code) DO UPDATE
			SET name = EXCLUDED.name,
			    generation_weight = EXCLUDED.generation_weight`,
			n.Code, n.Name, n.Weight); err != nil {
			log.Fatalf("upsert ref.nationalities %q: %v", n.Code, err)
		}
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM ref.name_pool WHERE nationality_code = ANY($1::text[])`, codes); err != nil {
		log.Fatalf("clear ref.name_pool: %v", err)
	}

	batch := &pgx.Batch{}
	first := true
	for _, code := range codes {
		for _, nameList := range [][]string{data.Generator.NameList(code, true), data.Generator.NameList(code, false)} {
			nameType := "last"
			if first {
				nameType = "first"
			}
			for _, name := range nameList {
				batch.Queue(`INSERT INTO ref.name_pool (nationality_code, name_type, name, frequency_weight) VALUES ($1, $2, $3, 1.0)`, code, nameType, name)
				names++
				if first {
					firstCount++
				} else {
					lastCount++
				}
			}
			first = !first
		}
	}

	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		log.Fatalf("insert ref.name_pool: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		log.Fatalf("commit: %v", err)
	}

	log.Printf("seeded ref.nationalities=%d ref.name_pool=%d (first=%d last=%d)", len(data.Nationalities), names, firstCount, lastCount)
}
