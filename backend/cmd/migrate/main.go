package main

import (
	"log"

	"lifeos-ai/backend/internal/config"
	"lifeos-ai/backend/internal/database"
	"lifeos-ai/backend/migrations"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	db, err := database.Open(cfg.DB.MigrationDSN())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ran, err := database.Migrate(db, migrations.FS)
	for _, v := range ran {
		log.Printf("applied %s", v)
	}
	if err != nil {
		log.Fatal(err)
	}
	if len(ran) == 0 {
		log.Println("database is up to date")
	}
}
