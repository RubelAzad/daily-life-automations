// Command worker runs background jobs separately from the API: reminder
// delivery, scheduled AI briefings and search indexing. Run with -once to process due work
// and exit.
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

	"lifeos-ai/backend/internal/ai"
	"lifeos-ai/backend/internal/automation"
	"lifeos-ai/backend/internal/config"
	"lifeos-ai/backend/internal/database"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/services"
	"lifeos-ai/backend/internal/storage"
)

func main() {
	once := flag.Bool("once", false, "process due work once and exit")
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

	// Scheduled briefings use the same services as the API.
	tasks := repository.TaskRepository{DB: db}
	events, reminderRepo, notes := repository.CalendarEventRepository{DB: db}, repository.ReminderRepository{DB: db}, repository.NoteRepository{DB: db}
	calendar := services.CalendarService{Events: events, Reminders: reminderRepo}
	deps := ai.Deps{
		Tasks:    services.TaskService{Tasks: tasks, Lists: repository.TaskListRepository{DB: db}, Projects: repository.ProjectRepository{DB: db}},
		Calendar: calendar,
		Today:    services.TodayService{Tasks: tasks, Calendar: calendar, Reminders: reminderRepo, Notes: notes},
	}
	aiService := ai.NewService(
		ai.NewClient(cfg.AI.Provider, cfg.AI.APIKey, cfg.AI.Model, cfg.AI.BaseURL, cfg.AI.MaxTokens, cfg.AI.Timeout),
		ai.Executor{Tools: ai.NewRegistry(), Actions: repository.AIActionRepository{DB: db}, Settings: repository.SettingsRepository{DB: db}, Log: logger},
		repository.AIConversationRepository{DB: db}, repository.UserRepository{DB: db},
	)
	briefings := automation.BriefingWorker{
		Briefer: ai.NewBriefer(aiService, deps, repository.BriefingRepository{DB: db}, repository.NotificationRepository{DB: db}),
		Log:     logger,
	}
	search := automation.SearchIndexWorker{
		Search: services.SearchService{
			Search: repository.SearchRepository{DB: db}, Notes: notes, Files: repository.FileRepository{DB: db},
			Store: storage.LocalStore{Root: cfg.StorageDir}, Log: logger,
		},
		Log: logger,
	}
	if !aiService.Status().Enabled {
		logger.Printf("AI disabled: scheduled briefings are off")
	}

	if *once {
		stats, err := reminders.RunOnce(context.Background())
		if err != nil {
			logger.Fatal(err)
		}
		logger.Printf("reminders: delivered=%d skipped=%d held=%d errors=%d", stats.Delivered, stats.Skipped, stats.Held, stats.Errors)
		bs, err := briefings.Briefer.RunScheduled(context.Background())
		if err != nil {
			logger.Fatal(err)
		}
		logger.Printf("briefings: generated=%d failed=%d", bs.Generated, bs.Failed)
		files, notesIndexed, err := search.Search.IndexPending(context.Background(), 200)
		if err != nil {
			logger.Fatal(err)
		}
		logger.Printf("search: indexed files=%d notes=%d", files, notesIndexed)
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
	logger.Printf("started; running jobs every %s", interval)
	automation.Run(ctx, interval, reminders, briefings, search)
	logger.Println("stopped")
}
