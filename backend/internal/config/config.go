package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv    string
	Port      string
	WebOrigin string
	DB        DBConfig
	// StorageDir holds uploaded files (local development storage).
	StorageDir     string
	MaxUploadBytes int64
}

type DBConfig struct {
	Host     string
	Port     string
	Name     string
	User     string
	Password string
}

// DSN builds the driver DSN from discrete fields so passwords containing
// special characters (@, #, /, ...) never need manual escaping.
func (d DBConfig) DSN() string { return d.dsn(false) }

// MigrationDSN additionally allows multiple statements per Exec, which
// migration files need. Do not use it for request handling.
func (d DBConfig) MigrationDSN() string { return d.dsn(true) }

func (d DBConfig) dsn(multiStatements bool) string {
	cfg := mysql.NewConfig()
	cfg.User = d.User
	cfg.Passwd = d.Password
	cfg.Net = "tcp"
	cfg.Addr = d.Host + ":" + d.Port
	cfg.DBName = d.Name
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.MultiStatements = multiStatements
	// Keep the session in UTC so NOW() and TIMESTAMP columns match Go's times.
	cfg.Params = map[string]string{"charset": "utf8mb4", "time_zone": "'+00:00'"}
	return cfg.FormatDSN()
}

func Load() (Config, error) {
	_ = godotenv.Load()

	cfg := Config{
		AppEnv:    getEnv("APP_ENV", "local"),
		Port:      getEnv("PORT", "8080"),
		WebOrigin: getEnv("WEB_ORIGIN", "http://localhost:3000"),
		DB: DBConfig{
			Host:     getEnv("DB_HOST", "127.0.0.1"),
			Port:     getEnv("DB_PORT", "3306"),
			Name:     os.Getenv("DB_NAME"),
			User:     os.Getenv("DB_USER"),
			Password: os.Getenv("DB_PASSWORD"),
		},
	}

	if cfg.DB.Name == "" || cfg.DB.User == "" {
		return cfg, fmt.Errorf("DB_NAME and DB_USER are required")
	}

	cfg.StorageDir = getEnv("STORAGE_DIR", "storage")
	maxMB, err := strconv.Atoi(getEnv("MAX_UPLOAD_MB", "20"))
	if err != nil || maxMB < 1 || maxMB > 500 {
		return cfg, fmt.Errorf("MAX_UPLOAD_MB must be a number between 1 and 500")
	}
	cfg.MaxUploadBytes = int64(maxMB) << 20
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
