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
		Host:     "localhost",
		Port:     5432,
		Name:     "netspire",
		User:     "netspire",
		Password: "password",
		SSLMode:  "disable",
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
	billingService := billing.New(db, logger)

	sessionService := session.New(db, logger, session.Config{
		NetFlowPort: 2055,
	})

	ippoolService := ippool.New(rdb, logger, ippool.Config{
		RedisPrefix: "ippool:",
	})

	disconnectService := disconnect.New(db, logger, disconnect.Config{
		CoAPort:    3799,
		CoASecret:  "secret",
		ScriptPath: "/opt/billing/scripts",
	})

	tclassService := tclass.New(logger, tclass.Config{
		ConfigFile: "tclass.yaml",
	})

	// Initialize handlers
	adminHandler := handlers.NewAdminHandler(billingService, logger)
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
		api.POST("/session/update", sessionHandler.UpdateSession)
		api.POST("/session/stop", sessionHandler.StopSession)
		api.GET("/session/:id", sessionHandler.GetSession)

		// IP Pool routes
		api.POST("/ippool/lease", ippoolHandler.LeaseIP)
		api.POST("/ippool/renew", ippoolHandler.RenewLease)
		api.POST("/ippool/release", ippoolHandler.ReleaseLease)
		api.GET("/ippool/status", ippoolHandler.GetPoolStatus)

		// Disconnect routes
		api.POST("/disconnect/coa", disconnectHandler.DisconnectCoA)
		api.POST("/disconnect/script", disconnectHandler.DisconnectScript)
		api.POST("/disconnect/pod", disconnectHandler.DisconnectPOD)

		// Traffic Classification routes
		api.POST("/tclass/classify", tclassHandler.ClassifyTraffic)
		api.GET("/tclass/config", tclassHandler.GetConfig)
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
