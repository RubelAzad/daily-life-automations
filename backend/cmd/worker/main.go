// Command worker runs background jobs separately from the API, from the
// durable scheduler (scheduled_jobs): reminder delivery, scheduled AI
// briefings, search indexing, automations and any other due job. Run with -once to run
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
	"lifeos-ai/backend/internal/approvals"
	"lifeos-ai/backend/internal/automation"
	"lifeos-ai/backend/internal/config"
	"lifeos-ai/backend/internal/database"
	"lifeos-ai/backend/internal/events"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/scheduler"
	"lifeos-ai/backend/internal/services"
	"lifeos-ai/backend/internal/storage"
	"lifeos-ai/backend/internal/workflow"
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

	// Briefings and automations use the same services as the API. Changes
	// that automations make publish events too, so automations can chain.
	bus := events.Bus{}
	tasks := repository.TaskRepository{DB: db}
	calEvents, reminderRepo, notes := repository.CalendarEventRepository{DB: db}, repository.ReminderRepository{DB: db}, repository.NoteRepository{DB: db}
	calendar := services.CalendarService{Events: calEvents, Reminders: reminderRepo}
	taskService := services.TaskService{Tasks: tasks, Lists: repository.TaskListRepository{DB: db}, Projects: repository.ProjectRepository{DB: db}, Events: bus}
	searchService := services.SearchService{
		Search: repository.SearchRepository{DB: db}, Notes: notes, Files: repository.FileRepository{DB: db},
		Store: storage.LocalStore{Root: cfg.StorageDir}, Log: logger,
	}
	deps := ai.Deps{
		Tasks:    taskService,
		Calendar: calendar,
		Today:    services.TodayService{Tasks: tasks, Calendar: calendar, Reminders: reminderRepo, Notes: notes},
		Notes: services.NoteService{
			Notes: notes, Files: repository.FileRepository{DB: db}, Projects: repository.ProjectRepository{DB: db}, Reminders: reminderRepo,
			Index: searchService, Events: bus,
		},
		Reminders:     services.ReminderService{Reminders: reminderRepo, Tasks: tasks, Events: calEvents, Notes: notes},
		Notifications: repository.NotificationRepository{DB: db},
		Settings:      repository.SettingsRepository{DB: db},
		Briefings:     repository.BriefingRepository{DB: db},
	}
	deps.Finance = services.FinanceService{Finance: repository.FinanceRepository{DB: db}, Settings: repository.SettingsRepository{DB: db}, Tasks: taskService, Events: bus}
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
		Search:    searchService,
		Log:       logger,
		Interval:  interval,
		Verbose:   *once,
	}
	sched := scheduler.New(db, logger)
	jobs.Register(sched)
	// plan_day writes briefings with the same briefer as the scheduled ones.
	automationDeps := deps
	automationDeps.Planner = jobs.Briefer
	automationTools := ai.AutomationTools(automationDeps)
	engine := workflow.Engine{
		DB: db, Settings: repository.SettingsRepository{DB: db}, Events: bus, Log: logger,
		Executor: ai.Executor{Tools: automationTools, Actions: repository.AIActionRepository{DB: db}, Settings: repository.SettingsRepository{DB: db}, Log: logger},
	}
	engine.Register(sched, interval)
	// Bill reminders: bill.due_soon for automations such as "Bill reminders"
	// (once per bill and due date: repeats are dropped by the event's key).
	sched.Every(scheduler.Repeating{Kind: "finance.scan_bills", Every: interval, Handler: func(ctx context.Context, _ scheduler.Job) error {
		_, err := deps.Finance.ScanBills(ctx)
		return err
	}})
	// Expiring only marks requests; it never runs a tool.
	approvals.Service{DB: db, Executor: engine.Executor, Engine: engine, Log: logger}.Register(sched, interval)
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
