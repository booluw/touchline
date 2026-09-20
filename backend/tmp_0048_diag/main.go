package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
)

func main() {
	ctx := context.Background()
	url := os.Getenv("DATABASE_URL")
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		fmt.Println("connect:", err)
		os.Exit(1)
	}
	defer conn.Close(ctx)

	var sp string
	conn.QueryRow(ctx, "SHOW search_path").Scan(&sp)
	fmt.Println("search_path:", sp)

	rows, err := conn.Query(ctx, `
		SELECT c.relname, n.nspname, i.indexdef
		FROM pg_indexes i
		JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_class t ON t.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE t.relname = 'club_name_parts' AND t.relnamespace = 'ref'::regnamespace
		ORDER BY c.relname`)
	if err != nil {
		fmt.Println("indexes:", err)
		os.Exit(1)
	}
	for rows.Next() {
		var name, nsp, def string
		if err := rows.Scan(&name, &nsp, &def); err != nil {
			panic(err)
		}
		fmt.Printf("index %s.%s: %s\n", nsp, name, def)
	}
	rows.Close()

	rows2, err := conn.Query(ctx, `
		SELECT n.nspname, c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
		WHERE c.relname = 'idx_club_name_parts_kind'`)
	if err != nil {
		fmt.Println("by-name:", err)
		os.Exit(1)
	}
	for rows2.Next() {
		var nsp, name string
		if err := rows2.Scan(&nsp, &name); err != nil {
			panic(err)
		}
		fmt.Printf("by-name: %s.%s\n", nsp, name)
	}
	rows2.Close()
}