package main

import (
	"context"
	"log"

	"github.com/gin-gonic/gin"

	"lifeos-ai/backend/internal/config"
	"lifeos-ai/backend/internal/database"
	"lifeos-ai/backend/internal/handlers"
	"lifeos-ai/backend/internal/middleware"
	"lifeos-ai/backend/internal/models"
	"lifeos-ai/backend/internal/repository"
	"lifeos-ai/backend/internal/services"
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

	authService := services.AuthService{
		Users:      repository.UserRepository{DB: db},
		Sessions:   repository.SessionRepository{DB: db},
		OnRegister: []func(context.Context, models.User) error{taskListService.CreateDefaults},
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

	protected.GET("/tasks", taskHandler.List)
	protected.POST("/tasks", taskHandler.Create)
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

	log.Printf("LifeOS API listening on :%s", cfg.Port)
	log.Fatal(router.Run(":" + cfg.Port))
}
