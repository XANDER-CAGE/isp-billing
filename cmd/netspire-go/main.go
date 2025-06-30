package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v3"

	"netspire-go/internal/database"
	"netspire-go/internal/handlers"
	"netspire-go/internal/models"
	"netspire-go/internal/services/billing"
	"netspire-go/internal/services/billing/tclass"
	"netspire-go/internal/services/disconnect"
	"netspire-go/internal/services/ippool"
	"netspire-go/internal/services/session"
)

type Config struct {
	Server struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
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
		MaxRetries int    `yaml:"max_retries"`
		PoolSize   int    `yaml:"pool_size"`
	} `yaml:"redis"`

	IPPool struct {
		Enabled               bool                `yaml:"enabled"`
		Timeout               int                 `yaml:"timeout"`
		UseAnotherOneFreePool bool                `yaml:"use_another_one_free_pool"`
		Pools                 []models.PoolConfig `yaml:"pools"`
	} `yaml:"ippool"`

	Session struct {
		Timeout         int `yaml:"timeout"`
		CleanupInterval int `yaml:"cleanup_interval"`
		SyncInterval    int `yaml:"sync_interval"`
		BatchSize       int `yaml:"batch_size"`
	} `yaml:"session"`

	Disconnect struct {
		Enabled bool              `yaml:"enabled"`
		Radius  disconnect.Config `yaml:"radius"`
		Script  disconnect.Config `yaml:"script"`
		Pod     disconnect.Config `yaml:"pod"`
	} `yaml:"disconnect"`

	Billing struct {
		Algorithms map[string]interface{} `yaml:"algorithms"`
	} `yaml:"billing"`

	TrafficClassification struct {
		Enabled        bool                  `yaml:"enabled"`
		DefaultClass   string                `yaml:"default_class"`
		ReloadInterval int                   `yaml:"reload_interval"`
		Classes        []tclass.ClassConfig  `yaml:"classes"`
		ProtocolRules  []tclass.ProtocolRule `yaml:"protocol_rules"`
	} `yaml:"traffic_classification"`

	Logging struct {
		Level  string `yaml:"level"`
		Format string `yaml:"format"`
		Output string `yaml:"output"`
	} `yaml:"logging"`
}

func main() {
	// Load configuration
	cfg, err := loadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Setup logging
	logger, err := setupLogging(cfg.Logging)
	if err != nil {
		log.Fatalf("Failed to setup logging: %v", err)
	}
	defer logger.Sync()

	logger.Info("Starting Netspire-Go Billing System")

	// Initialize database
	db, err := database.NewPostgreSQL(database.Config{
		Host:               cfg.Database.Host,
		Port:               cfg.Database.Port,
		Name:               cfg.Database.Name,
		User:               cfg.Database.User,
		Password:           cfg.Database.Password,
		SSLMode:            cfg.Database.SSLMode,
		MaxConnections:     cfg.Database.MaxConnections,
		MaxIdleConnections: cfg.Database.MaxIdleConnections,
	})
	if err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
	}
	defer db.Close()

	// Initialize Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr:       fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
		Password:   cfg.Redis.Password,
		DB:         cfg.Redis.DB,
		MaxRetries: cfg.Redis.MaxRetries,
		PoolSize:   cfg.Redis.PoolSize,
	})
	defer redisClient.Close()

	// Test Redis connection
	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Fatal("Failed to connect to Redis", zap.Error(err))
	}

	// Initialize services

	// Traffic Classification Service
	var tclassService *tclass.Service
	var protocolClassifier *tclass.ProtocolClassifier
	var enhancedClassifier *tclass.EnhancedClassifier

	if cfg.TrafficClassification.Enabled {
		tclassService = tclass.New(logger)
		protocolClassifier = tclass.NewProtocolClassifier(logger)
		enhancedClassifier = tclass.NewEnhancedClassifier(tclassService, protocolClassifier, logger)

		// Load traffic classification rules
		if len(cfg.TrafficClassification.Classes) > 0 {
			if err := tclassService.Load(cfg.TrafficClassification.Classes); err != nil {
				logger.Error("Failed to load traffic classification rules", zap.Error(err))
			} else {
				logger.Info("Traffic classification rules loaded", zap.Int("classes", len(cfg.TrafficClassification.Classes)))
			}
		} else {
			// Load default configuration
			defaultConfig := tclass.GetDefaultConfig()
			if err := tclassService.Load(defaultConfig.Classes); err != nil {
				logger.Error("Failed to load default traffic classification rules", zap.Error(err))
			} else {
				logger.Info("Default traffic classification rules loaded")
			}
		}

		// Load protocol rules
		if len(cfg.TrafficClassification.ProtocolRules) > 0 {
			protocolClassifier.LoadRulesFromConfig(cfg.TrafficClassification.ProtocolRules)
		}

		logger.Info("Traffic classification service started")
	}

	// IP Pool Service
	var ippoolService *ippool.Service
	if cfg.IPPool.Enabled {
		ippoolOptions := map[string]interface{}{
			"timeout":                   cfg.IPPool.Timeout,
			"use_another_one_free_pool": "yes",
		}
		if !cfg.IPPool.UseAnotherOneFreePool {
			ippoolOptions["use_another_one_free_pool"] = "no"
		}

		ippoolService = ippool.New(redisClient, logger, ippoolOptions)

		// Initialize IP pools
		if err := ippoolService.Start(cfg.IPPool.Pools); err != nil {
			logger.Fatal("Failed to start IP pool service", zap.Error(err))
		}

		logger.Info("IP Pool service started", zap.Int("pools", len(cfg.IPPool.Pools)))
	}

	// Session Service
	sessionService := session.New(redisClient, logger, cfg.Session.Timeout)
	logger.Info("Session service started")

	// Disconnect Service
	var disconnectService *disconnect.Service
	if cfg.Disconnect.Enabled {
		disconnectService = disconnect.New(logger, cfg.Disconnect.Radius)
		logger.Info("Disconnect service started")
	}

	// Billing Service
	billingService := billing.NewService(db, cfg.Billing.Algorithms)
	logger.Info("Billing service started")

	// Initialize RADIUS handler
	radiusHandler := handlers.NewSimpleRADIUSHandler(logger)

	// Setup HTTP routes
	router := setupRouter(logger, sessionService, ippoolService, disconnectService, billingService, tclassService, enhancedClassifier, radiusHandler)

	// Start session cleanup routine
	go func() {
		ticker := time.NewTicker(time.Duration(cfg.Session.CleanupInterval) * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			if err := sessionService.CleanupExpiredSessions(); err != nil {
				logger.Error("Failed to cleanup expired sessions", zap.Error(err))
			}
		}
	}()

	// Start HTTP server
	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler: router,
	}

	go func() {
		logger.Info("Starting HTTP server",
			zap.String("host", cfg.Server.Host),
			zap.Int("port", cfg.Server.Port))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Failed to start HTTP server", zap.Error(err))
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down gracefully...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server stopped")
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
	Output string `yaml:"output"`
}) (*zap.Logger, error) {
	level := zap.InfoLevel
	switch cfg.Level {
	case "debug":
		level = zap.DebugLevel
	case "warn":
		level = zap.WarnLevel
	case "error":
		level = zap.ErrorLevel
	}

	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(level)

	if cfg.Format == "json" {
		config.Encoding = "json"
	} else {
		config.Encoding = "console"
		config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	}

	return config.Build()
}

func setupRouter(logger *zap.Logger, sessionService *session.Service, ippoolService *ippool.Service, disconnectService *disconnect.Service, billingService *billing.Service, tclassService *tclass.Service, enhancedClassifier *tclass.EnhancedClassifier, radiusHandler *handlers.SimpleRADIUSHandler) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()

	// Middleware
	router.Use(gin.Recovery())

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "timestamp": time.Now().Unix()})
	})

	// Session management endpoints
	v1 := router.Group("/api/v1")
	{
		// Session endpoints
		sessionGroup := v1.Group("/sessions")
		{
			sessionGroup.GET("/", func(c *gin.Context) {
				sessions, err := sessionService.List()
				if err != nil {
					logger.Error("Failed to list sessions", zap.Error(err))
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"sessions": sessions, "count": len(sessions)})
			})

			sessionGroup.GET("/:sid", func(c *gin.Context) {
				sid := c.Param("sid")
				session, err := sessionService.FindBySID(sid)
				if err != nil {
					c.JSON(http.StatusNotFound, gin.H{"error": "Session not found"})
					return
				}
				c.JSON(http.StatusOK, session)
			})

			sessionGroup.DELETE("/:sid", func(c *gin.Context) {
				sid := c.Param("sid")
				_, err := sessionService.Stop(sid)
				if err != nil {
					logger.Error("Failed to stop session", zap.String("sid", sid), zap.Error(err))
					c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusOK, gin.H{"message": "Session stopped"})
			})
		}

		// IP Pool endpoints
		if ippoolService != nil {
			ippoolGroup := v1.Group("/ippool")
			{
				ippoolGroup.GET("/info", func(c *gin.Context) {
					entries, err := ippoolService.Info()
					if err != nil {
						logger.Error("Failed to get IP pool info", zap.Error(err))
						c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
						return
					}
					c.JSON(http.StatusOK, gin.H{"entries": entries, "count": len(entries)})
				})

				ippoolGroup.POST("/lease/:pool", func(c *gin.Context) {
					pool := c.Param("pool")
					ip, err := ippoolService.Lease(pool)
					if err != nil {
						logger.Error("Failed to lease IP", zap.String("pool", pool), zap.Error(err))
						c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
						return
					}
					c.JSON(http.StatusOK, gin.H{"ip": ip.String(), "pool": pool})
				})

				ippoolGroup.POST("/release", func(c *gin.Context) {
					var req struct {
						IP string `json:"ip" binding:"required"`
					}
					if err := c.ShouldBindJSON(&req); err != nil {
						c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
						return
					}

					ip := net.ParseIP(req.IP)
					if ip == nil {
						c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid IP address"})
						return
					}

					if err := ippoolService.Release(ip); err != nil {
						logger.Error("Failed to release IP", zap.String("ip", req.IP), zap.Error(err))
						c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
						return
					}
					c.JSON(http.StatusOK, gin.H{"message": "IP released"})
				})
			}
		}

		// Disconnect endpoints
		if disconnectService != nil {
			disconnectGroup := v1.Group("/disconnect")
			{
				disconnectGroup.POST("/session/:sid", func(c *gin.Context) {
					sid := c.Param("sid")
					session, err := sessionService.FindBySID(sid)
					if err != nil {
						c.JSON(http.StatusNotFound, gin.H{"error": "Session not found"})
						return
					}

					if err := disconnectService.DisconnectSession(session); err != nil {
						logger.Error("Failed to disconnect session", zap.String("sid", sid), zap.Error(err))
						c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
						return
					}
					c.JSON(http.StatusOK, gin.H{"message": "Disconnect request sent"})
				})
			}
		}

		// Traffic classification endpoints
		if tclassService != nil && enhancedClassifier != nil {
			tclassGroup := v1.Group("/tclass")
			{
				tclassGroup.GET("/stats", func(c *gin.Context) {
					stats := tclassService.GetStats()
					c.JSON(http.StatusOK, gin.H{
						"classification_stats": stats,
						"timestamp":            time.Now().Unix(),
					})
				})

				tclassGroup.POST("/classify", func(c *gin.Context) {
					var req struct {
						SrcIP   string `json:"src_ip" binding:"required"`
						DstIP   string `json:"dst_ip" binding:"required"`
						SrcPort uint16 `json:"src_port"`
						DstPort uint16 `json:"dst_port"`
					}

					if err := c.ShouldBindJSON(&req); err != nil {
						c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
						return
					}

					srcIP := net.ParseIP(req.SrcIP)
					dstIP := net.ParseIP(req.DstIP)
					if srcIP == nil || dstIP == nil {
						c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid IP addresses"})
						return
					}

					classification := enhancedClassifier.ClassifyTraffic(srcIP, dstIP, req.SrcPort, req.DstPort)
					c.JSON(http.StatusOK, classification)
				})

				tclassGroup.POST("/test", func(c *gin.Context) {
					tclassService.TestClassification()
					c.JSON(http.StatusOK, gin.H{"message": "Test classification completed, check logs for results"})
				})

				tclassGroup.GET("/classify/:ip", func(c *gin.Context) {
					ipStr := c.Param("ip")
					ip := net.ParseIP(ipStr)
					if ip == nil {
						c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid IP address"})
						return
					}

					class, found := tclassService.ClassifyIP(ip)
					if !found {
						class = tclass.ClassDefault
					}

					c.JSON(http.StatusOK, gin.H{
						"ip":    ipStr,
						"class": string(class),
						"found": found,
					})
				})
			}
		}

		// RADIUS integration routes
		radiusHandler.RegisterRoutes(v1)
	}

	return router
}
