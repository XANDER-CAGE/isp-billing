package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"

	"isp-billing/internal/database"
	"isp-billing/internal/handlers"
	"isp-billing/internal/services/billing"
	"isp-billing/internal/services/disconnect"
	"isp-billing/internal/services/ippool"
	"isp-billing/internal/services/session"
	"isp-billing/internal/services/tclass"
)

func main() {
	// Setup logging with Zap
	logger := setupZapLogging()
	defer logger.Sync()

	logger.Info("Starting ISP Billing System")

	// Initialize database
	dbConfig := database.Config{
		Host:     getEnv("DB_HOST", "localhost"),
		Port:     getEnvInt("DB_PORT", 5432),
		Name:     getEnv("DB_NAME", "netspire"),
		User:     getEnv("DB_USER", "netspire"),
		Password: getEnv("DB_PASSWORD", "password"),
		SSLMode:  getEnv("DB_SSLMODE", "disable"),
	}

	db, err := database.NewPostgreSQL(dbConfig)
	if err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
	}
	defer db.Close()

	// Initialize Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})
	defer rdb.Close()

	// Test Redis connection
	ctx := context.Background()
	_, err = rdb.Ping(ctx).Result()
	if err != nil {
		logger.Fatal("Failed to connect to Redis", zap.Error(err))
	}

	// Initialize services
	billingService := billing.NewService(db, map[string]interface{}{})

	ippoolService := ippool.New(rdb, logger, ippool.Config{})

	disconnectService := disconnect.New(logger, disconnect.Config{
		RADIUSEnabled: true,
		Secret:        "secret",
		ScriptEnabled: true,
		ScriptPath:    "/opt/billing/scripts",
	})

	sessionService := session.New(rdb, db, billingService, ippoolService, disconnectService, logger, session.Config{
		SessionTimeout: 3600,
		SyncInterval:   30,
	})

	tclassService := tclass.New(logger, tclass.Config{
		ConfigFile: "tclass.yaml",
	})

	// Initialize handlers
	adminHandler := handlers.NewAdminHandler(db)
	sessionHandler := handlers.NewSessionHandler(sessionService, logger)
	ippoolHandler := handlers.NewIPPoolHandler(ippoolService, logger)
	disconnectHandler := handlers.NewDisconnectHandler(disconnectService, logger)
	tclassHandler := handlers.NewTClassHandler(tclassService, logger)

	// Setup Gin router
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// API routes
	api := router.Group("/api/v1")
	{
		// Admin routes
		api.GET("/accounts/:id", adminHandler.GetAccount)
		api.POST("/accounts/:id/charge", adminHandler.ChargeAccount)
		api.GET("/accounts/:id/balance", adminHandler.GetBalance)

		// Session routes
		api.POST("/session/start", sessionHandler.StartSession)
		api.POST("/session/update", sessionHandler.InterimUpdate)
		api.POST("/session/stop", sessionHandler.StopSession)
		api.GET("/session/:id", sessionHandler.GetSession)

		// IP Pool routes
		api.POST("/ippool/lease", ippoolHandler.LeaseIP)
		api.POST("/ippool/renew", ippoolHandler.RenewIP)
		api.POST("/ippool/release", ippoolHandler.ReleaseIP)
		api.GET("/ippool/info", ippoolHandler.GetPoolInfo)

		// Disconnect routes
		api.POST("/disconnect/session", disconnectHandler.DisconnectSession)
		api.POST("/disconnect/ip", disconnectHandler.DisconnectByIP)

		// Traffic Classification routes
		api.GET("/tclass/classify/:ip", tclassHandler.ClassifyIP)
		api.GET("/tclass/classes", tclassHandler.GetAllClasses)
		api.POST("/tclass/reload", tclassHandler.ReloadConfig)
	}

	// Start HTTP server
	server := &http.Server{
		Addr:         ":8080",
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Start server in a goroutine
	go func() {
		logger.Info("Starting HTTP server on :8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Failed to start server", zap.Error(err))
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	// Give outstanding requests a deadline for completion
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server exiting")
}

func setupZapLogging() *zap.Logger {
	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(zap.InfoLevel)

	logger, err := config.Build()
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}

	return logger
}
