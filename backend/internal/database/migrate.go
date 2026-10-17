package database

import (
	"database/sql"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// Migrate applies every *.up.sql file in files that is not yet recorded in
// schema_migrations, in filename order. The connection must allow
// multiStatements. Returns the versions applied in this run.
func Migrate(db *sql.DB, files fs.FS) ([]string, error) {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version VARCHAR(255) NOT NULL PRIMARY KEY,
		applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`); err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}

	applied := map[string]bool{}
	rows, err := db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return nil, err
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	names, err := fs.Glob(files, "*.up.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)

	var ran []string
	for _, name := range names {
		version := strings.TrimSuffix(name, ".up.sql")
		if applied[version] {
			continue
		}
		body, err := fs.ReadFile(files, name)
		if err != nil {
			return ran, err
		}
		if _, err := db.Exec(string(body)); err != nil {
			return ran, fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := db.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
			return ran, fmt.Errorf("record %s: %w", name, err)
		}
		ran = append(ran, version)
	}
	return ran, nil
}
