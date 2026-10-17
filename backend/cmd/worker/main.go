// Command worker runs background jobs (currently: reminder delivery),
// separately from the API. Run with -once to process due work and exit.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"lifeos-ai/backend/internal/automation"
	"lifeos-ai/backend/internal/config"
	"lifeos-ai/backend/internal/database"
)

func main() {
	once := flag.Bool("once", false, "process due reminders once and exit")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	db, err := database.Open(cfg.DB.DSN())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	logger := log.New(os.Stdout, "worker ", log.LstdFlags)
	reminders := automation.ReminderWorker{DB: db, Log: logger}

	if *once {
		stats, err := reminders.RunOnce(context.Background())
		if err != nil {
			logger.Fatal(err)
		}
		logger.Printf("reminders: delivered=%d skipped=%d errors=%d", stats.Delivered, stats.Skipped, stats.Errors)
		return
	}

	interval := 30 * time.Second
	if v := os.Getenv("WORKER_INTERVAL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			interval = time.Duration(n) * time.Second
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Printf("started; checking reminders every %s", interval)
	reminders.Run(ctx, interval)
	logger.Println("stopped")
}
