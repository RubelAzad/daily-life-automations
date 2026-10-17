// Command worker runs background jobs separately from the API, from the
// durable scheduler (scheduled_jobs): reminder delivery, scheduled AI
// briefings, search indexing and any other due job. Run with -once to run
// the system jobs and whatever else is due, then exit.
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
	"lifeos-ai/backend/internal/scheduler"
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
	interval := 30 * time.Second
	if v := os.Getenv("WORKER_INTERVAL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			interval = time.Duration(n) * time.Second
		}
	}

	jobs := automation.SystemJobs{
		DB:        db,
		Reminders: reminders,
		Briefer:   ai.NewBriefer(aiService, deps, repository.BriefingRepository{DB: db}, repository.NotificationRepository{DB: db}),
		Search: services.SearchService{
			Search: repository.SearchRepository{DB: db}, Notes: notes, Files: repository.FileRepository{DB: db},
			Store: storage.LocalStore{Root: cfg.StorageDir}, Log: logger,
		},
		Log:      logger,
		Interval: interval,
		Verbose:  *once,
	}
	sched := scheduler.New(db, logger)
	jobs.Register(sched)
	if !aiService.Status().Enabled {
		logger.Printf("AI disabled: scheduled briefings are off")
	}
	if err := sched.Start(context.Background()); err != nil {
		logger.Fatal(err)
	}

	if *once {
		// Run every system job now, plus any other job that is due.
		if err := sched.RunNow(context.Background()); err != nil {
			logger.Fatal(err)
		}
		stats, err := sched.Drain(context.Background())
		if err != nil {
			logger.Fatal(err)
		}
		logger.Printf("jobs: succeeded=%d retrying=%d failed=%d", stats.Succeeded, stats.Retrying, stats.Failed)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Printf("started as %s; system jobs every %s", sched.WorkerID, interval)
	sched.Loop(ctx, min(interval, 2*time.Second))
	logger.Println("stopped")
}
