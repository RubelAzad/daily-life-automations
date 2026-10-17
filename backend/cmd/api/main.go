package main

import (
	"log"

	"github.com/gin-gonic/gin"

	"lifeos-ai/backend/internal/config"
	"lifeos-ai/backend/internal/database"
	"lifeos-ai/backend/internal/handlers"
	"lifeos-ai/backend/internal/middleware"
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

	authService := services.AuthService{
		Users:    repository.UserRepository{DB: db},
		Sessions: repository.SessionRepository{DB: db},
	}
	authHandler := handlers.AuthHandler{Auth: authService, SecureCookies: cfg.AppEnv != "local"}

	api := router.Group("/api/v1")
	api.GET("/health", handlers.HealthHandler{DB: db}.Get)
	api.POST("/auth/register", authHandler.Register)
	api.POST("/auth/login", authHandler.Login)
	api.POST("/auth/logout", authHandler.Logout)

	protected := api.Group("", middleware.RequireAuth(authService))
	protected.GET("/auth/me", authHandler.Me)

	log.Printf("LifeOS API listening on :%s", cfg.Port)
	log.Fatal(router.Run(":" + cfg.Port))
}
