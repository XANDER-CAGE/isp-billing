package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

	"netspire-go/internal/database"
	"netspire-go/internal/services/billing"
	"netspire-go/internal/services/netflow"
	"netspire-go/internal/services/radius"
	"netspire-go/internal/services/session"
)

type Config struct {
	Server struct {
		Host  string `yaml:"host"`
		Port  int    `yaml:"port"`
		Debug bool   `yaml:"debug"`
	} `yaml:"server"`

	Database struct {
		Host               string `yaml:"host"`
		Port               int    `yaml:"port"`
		Name               string `yaml:"name"`
		User               string `yaml:"user"`
		Password           string `yaml:"password"`
		SSLMode            string `yaml:"sslmode"`
		MaxConnections     int    `yaml:"max_connections"`
		MaxIdleConnections int    `yaml:"max_idle_connections"`
	} `yaml:"database"`

	Redis struct {
		Host       string `yaml:"host"`
		Port       int    `yaml:"port"`
		Password   string `yaml:"password"`
		DB         int    `yaml:"db"`
		SessionTTL int    `yaml:"session_ttl"`
	} `yaml:"redis"`

	NetFlow struct {
		ListenPort int `yaml:"listen_port"`
		BufferSize int `yaml:"buffer_size"`
		Workers    int `yaml:"workers"`
	} `yaml:"netflow"`

	RADIUS struct {
		AuthorizeEndpoint  string `yaml:"authorize_endpoint"`
		AccountingEndpoint string `yaml:"accounting_endpoint"`
		PostAuthEndpoint   string `yaml:"post_auth_endpoint"`
	} `yaml:"radius"`

	Session struct {
		Timeout            int `yaml:"timeout"`
		CleanupInterval    int `yaml:"cleanup_interval"`
		MaxSessionsPerUser int `yaml:"max_sessions_per_user"`
	} `yaml:"session"`

	Billing struct {
		Algorithms map[string]string `yaml:"algorithms"`
	} `yaml:"billing"`

	Logging struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
		File   string `yaml:"file"`
	} `yaml:"logging"`
}

func main() {
	// Load configuration
	cfg, err := loadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Setup logging
	setupLogging(cfg.Logging)

	logrus.Info("Starting Netspire-Go Billing System")

	// Initialize database
	db, err := database.NewPostgreSQL(cfg.Database)
	if err != nil {
		logrus.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Initialize Redis cache
	redisClient, err := cache.NewRedisClient(cfg.Redis)
	if err != nil {
		logrus.Fatalf("Failed to connect to Redis: %v", err)
	}
	defer redisClient.Close()

	// Initialize services
	sessionService := session.NewService(db, redisClient, cfg.Session)
	billingService := billing.NewService(db, cfg.Billing)
	radiusService := radius.NewService(db, sessionService, billingService)
	netflowService := netflow.NewService(sessionService, billingService, cfg.NetFlow)

	// Setup HTTP router
	router := setupRouter(cfg, radiusService)

	// Start NetFlow collector
	go func() {
		logrus.Infof("Starting NetFlow collector on port %d", cfg.NetFlow.ListenPort)
		if err := netflowService.Start(); err != nil {
			logrus.Fatalf("Failed to start NetFlow service: %v", err)
		}
	}()

	// Start session cleanup
	go sessionService.StartCleanup()

	// Start HTTP server
	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler: router,
	}

	go func() {
		logrus.Infof("Starting HTTP server on %s:%d", cfg.Server.Host, cfg.Server.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logrus.Fatalf("Failed to start HTTP server: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logrus.Info("Shutting down gracefully...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logrus.Errorf("Server forced to shutdown: %v", err)
	}

	logrus.Info("Server stopped")
}

func loadConfig(filename string) (*Config, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var cfg Config
	decoder := yaml.NewDecoder(file)
	err = decoder.Decode(&cfg)
	return &cfg, err
}

func setupLogging(cfg struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
	File   string `yaml:"file"`
}) {
	level, err := logrus.ParseLevel(cfg.Level)
	if err != nil {
		level = logrus.InfoLevel
	}
	logrus.SetLevel(level)

	if cfg.Format == "json" {
		logrus.SetFormatter(&logrus.JSONFormatter{})
	} else {
		logrus.SetFormatter(&logrus.TextFormatter{
			FullTimestamp: true,
		})
	}

	if cfg.File != "" {
		file, err := os.OpenFile(cfg.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err == nil {
			logrus.SetOutput(file)
		} else {
			logrus.Warnf("Failed to open log file %s: %v", cfg.File, err)
		}
	}
}

func setupRouter(cfg *Config, radiusService *radius.Service) *gin.Engine {
	if !cfg.Server.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "ok",
			"service":   "netspire-go",
			"timestamp": time.Now().Unix(),
		})
	})

	// RADIUS endpoints for FreeRADIUS integration
	v1 := router.Group("/api/v1")
	{
		v1.POST(cfg.RADIUS.AuthorizeEndpoint, radiusService.HandleAuthorize)
		v1.POST(cfg.RADIUS.AccountingEndpoint, radiusService.HandleAccounting)
		v1.POST(cfg.RADIUS.PostAuthEndpoint, radiusService.HandlePostAuth)
	}

	// Admin endpoints
	admin := router.Group("/admin")
	{
		admin.GET("/sessions", radiusService.ListSessions)
		admin.GET("/sessions/:sid", radiusService.GetSession)
		admin.DELETE("/sessions/:sid", radiusService.DisconnectSession)
		admin.GET("/stats", radiusService.GetStats)
	}

	return router
}
