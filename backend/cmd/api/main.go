package main

import (
	"context"
	"log"

	"github.com/gin-gonic/gin"

	"lifeos-ai/backend/internal/ai"
	"lifeos-ai/backend/internal/config"
	"lifeos-ai/backend/internal/database"
	"lifeos-ai/backend/internal/handlers"
	"lifeos-ai/backend/internal/middleware"
	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/services"
	"lifeos-ai/backend/internal/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	db, err := database.Open(cfg.DB.DSN())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if cfg.AppEnv != "local" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()
	if err := router.SetTrustedProxies(nil); err != nil {
		log.Fatal(err)
	}
	router.Use(middleware.CORS(cfg.WebOrigin))

	handlers.RegisterValidation()

	taskLists := repository.TaskListRepository{DB: db}
	taskListService := services.TaskListService{Lists: taskLists}
	projects := repository.ProjectRepository{DB: db}
	taskService := services.TaskService{Tasks: repository.TaskRepository{DB: db}, Lists: taskLists, Projects: projects}
	projectHandler := handlers.ProjectHandler{Projects: services.ProjectService{Projects: projects}}
	events := repository.CalendarEventRepository{DB: db}
	reminders := repository.ReminderRepository{DB: db}
	calendarService := services.CalendarService{Events: events, Reminders: reminders}
	calendarHandler := handlers.CalendarHandler{Calendar: calendarService}
	notes := repository.NoteRepository{DB: db}
	todayHandler := handlers.TodayHandler{Today: services.TodayService{
		Tasks: repository.TaskRepository{DB: db}, Calendar: calendarService, Reminders: reminders, Notes: notes,
	}}
	store := storage.LocalStore{Root: cfg.StorageDir}
	searchService := services.SearchService{
		Search: repository.SearchRepository{DB: db}, Notes: notes, Files: repository.FileRepository{DB: db}, Store: store,
	}
	searchHandler := handlers.SearchHandler{Service: searchService}
	noteHandler := handlers.NoteHandler{Notes: services.NoteService{
		Notes: notes, Files: repository.FileRepository{DB: db}, Projects: projects, Reminders: reminders,
		Store: store, MaxUploadBytes: cfg.MaxUploadBytes, Index: searchService,
	}}
	reminderHandler := handlers.ReminderHandler{
		Reminders:     services.ReminderService{Reminders: reminders, Tasks: repository.TaskRepository{DB: db}, Events: events, Notes: notes},
		Notifications: services.NotificationService{Notifications: repository.NotificationRepository{DB: db}},
	}

	settingsService := services.SettingsService{Settings: repository.SettingsRepository{DB: db}}
	settingsHandler := handlers.SettingsHandler{Settings: settingsService}
	aiService := ai.NewService(
		ai.NewClient(cfg.AI.Provider, cfg.AI.APIKey, cfg.AI.Model, cfg.AI.BaseURL, cfg.AI.MaxTokens, cfg.AI.Timeout),
		ai.Executor{
			Tools: ai.BuiltinTools(ai.Deps{
				Tasks: taskService, Calendar: calendarService, Notes: noteHandler.Notes, Reminders: reminderHandler.Reminders,
				Today: todayHandler.Today, Search: searchService,
			}),
			Actions:  repository.AIActionRepository{DB: db},
			Settings: repository.SettingsRepository{DB: db},
		},
		repository.AIConversationRepository{DB: db},
		repository.UserRepository{DB: db},
	)
	aiDeps := ai.Deps{
		Tasks: taskService, Calendar: calendarService, Notes: noteHandler.Notes, Reminders: reminderHandler.Reminders,
		Today: todayHandler.Today, Search: searchService,
	}
	aiHandler := handlers.AIHandler{
		AI:      aiService,
		Briefer: ai.NewBriefer(aiService, aiDeps, repository.BriefingRepository{DB: db}, repository.NotificationRepository{DB: db}),
	}
	if st := aiService.Status(); st.Enabled {
		log.Printf("AI enabled: provider=%s model=%s", st.Provider, st.Model)
	} else {
		log.Printf("AI disabled: set ANTHROPIC_API_KEY in .env to enable it")
	}

	authService := services.AuthService{
		Users:    repository.UserRepository{DB: db},
		Sessions: repository.SessionRepository{DB: db},
		OnRegister: []func(context.Context, models.User, services.RegisterOptions) error{
			func(ctx context.Context, u models.User, _ services.RegisterOptions) error {
				return taskListService.CreateDefaults(ctx, u)
			},
			settingsService.CreateDefaults,
		},
	}
	authHandler := handlers.AuthHandler{Auth: authService, SecureCookies: cfg.AppEnv != "local"}
	taskHandler := handlers.TaskHandler{Tasks: taskService}
	taskListHandler := handlers.TaskListHandler{Lists: taskListService}

	api := router.Group("/api/v1")
	api.GET("/health", handlers.HealthHandler{DB: db}.Get)
	api.POST("/auth/register", authHandler.Register)
	api.POST("/auth/login", authHandler.Login)
	api.POST("/auth/logout", authHandler.Logout)

	protected := api.Group("", middleware.RequireAuth(authService))
	protected.GET("/auth/me", authHandler.Me)
	protected.GET("/settings", settingsHandler.Get)
	protected.PATCH("/settings", settingsHandler.Update)

	protected.GET("/today", todayHandler.Get)

	protected.GET("/ai/status", aiHandler.Status)
	protected.POST("/ai/ping", aiHandler.Ping)
	protected.GET("/ai/actions", aiHandler.ListActions)
	protected.GET("/ai/actions/:id", aiHandler.GetAction)
	protected.POST("/ai/actions/:id/approve", aiHandler.Approve)
	protected.POST("/ai/actions/:id/reject", aiHandler.Reject)
	protected.GET("/ai/briefings/today", aiHandler.TodayBriefings)
	protected.GET("/ai/briefings", aiHandler.ListBriefings)
	protected.POST("/ai/briefings", aiHandler.CreateBriefing)
	protected.GET("/ai/conversations", aiHandler.ListConversations)
	protected.POST("/ai/conversations", aiHandler.CreateConversation)
	protected.GET("/ai/conversations/:id", aiHandler.GetConversation)
	protected.DELETE("/ai/conversations/:id", aiHandler.DeleteConversation)
	protected.POST("/ai/conversations/:id/messages", aiHandler.SendMessage)

	protected.GET("/tasks", taskHandler.List)
	protected.POST("/tasks", taskHandler.Create)
	protected.POST("/tasks/carry-forward", taskHandler.CarryForward)
	protected.GET("/tasks/:id", taskHandler.Get)
	protected.PATCH("/tasks/:id", taskHandler.Update)
	protected.DELETE("/tasks/:id", taskHandler.Delete)
	protected.POST("/tasks/:id/dependencies", taskHandler.AddDependency)
	protected.DELETE("/tasks/:id/dependencies/:depends_on_id", taskHandler.RemoveDependency)

	protected.GET("/task-lists", taskListHandler.List)
	protected.POST("/task-lists", taskListHandler.Create)
	protected.PATCH("/task-lists/:id", taskListHandler.Update)
	protected.DELETE("/task-lists/:id", taskListHandler.Delete)

	protected.GET("/projects", projectHandler.List)
	protected.POST("/projects", projectHandler.Create)
	protected.GET("/projects/:id", projectHandler.Get)
	protected.PATCH("/projects/:id", projectHandler.Update)
	protected.DELETE("/projects/:id", projectHandler.Delete)
	protected.POST("/projects/:id/milestones", projectHandler.CreateMilestone)
	protected.PATCH("/milestones/:id", projectHandler.UpdateMilestone)
	protected.DELETE("/milestones/:id", projectHandler.DeleteMilestone)

	protected.GET("/events", calendarHandler.List)
	protected.POST("/events", calendarHandler.Create)
	protected.GET("/events/:id", calendarHandler.Get)
	protected.PATCH("/events/:id", calendarHandler.Update)
	protected.DELETE("/events/:id", calendarHandler.Delete)

	protected.GET("/notes", noteHandler.List)
	protected.POST("/notes", noteHandler.Create)
	protected.GET("/notes/:id", noteHandler.Get)
	protected.PATCH("/notes/:id", noteHandler.Update)
	protected.DELETE("/notes/:id", noteHandler.Delete)
	protected.POST("/notes/:id/files", noteHandler.Upload)
	protected.GET("/files/:id/download", noteHandler.Download)
	protected.DELETE("/files/:id", noteHandler.DeleteFile)
	protected.GET("/search", searchHandler.Search)
	protected.GET("/tags", noteHandler.ListTags)
	protected.PATCH("/tags/:id", noteHandler.UpdateTag)
	protected.DELETE("/tags/:id", noteHandler.DeleteTag)

	protected.GET("/reminders", reminderHandler.List)
	protected.POST("/reminders", reminderHandler.Create)
	protected.PATCH("/reminders/:id", reminderHandler.Update)
	protected.DELETE("/reminders/:id", reminderHandler.Delete)
	protected.POST("/reminders/:id/snooze", reminderHandler.Snooze)

	protected.GET("/notifications", reminderHandler.ListNotifications)
	protected.GET("/notifications/unread-count", reminderHandler.UnreadCount)
	protected.POST("/notifications/read-all", reminderHandler.MarkAllRead)
	protected.POST("/notifications/:id/read", reminderHandler.MarkRead)

	log.Printf("LifeOS API listening on :%s", cfg.Port)
	log.Fatal(router.Run(":" + cfg.Port))
}
