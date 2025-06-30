package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"netspire-go/internal/database"
	"netspire-go/internal/services/billing"

	"go.uber.org/zap"
	"gopkg.in/yaml.v2"
)

// Config структура конфигурации
type Config struct {
	Database struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		Name     string `yaml:"name"`
		User     string `yaml:"user"`
		Password string `yaml:"password"`
		SSLMode  string `yaml:"sslmode"`
	} `yaml:"database"`

	Subscription billing.SubscriptionConfig `yaml:"subscription"`
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "process":
		processCommand()
	case "history":
		historyCommand()
	case "stats":
		statsCommand()
	case "help":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Netspire Subscription Processor

USAGE:
    subscription-processor <COMMAND> [OPTIONS]

COMMANDS:
    process [date]           Process monthly charges (YYYY-MM-DD or current date)
    history <account_id>     Show charge history for account
    stats                    Show billing statistics
    help                     Show this help message

EXAMPLES:
    subscription-processor process                    # Process for current month
    subscription-processor process 2024-01-01        # Process for January 2024
    subscription-processor history 123               # Show history for account 123
    subscription-processor stats                     # Show statistics
`)
}

func processCommand() {
	logger := createLogger()
	config := loadConfig()

	// Initialize database
	dbConfig := database.Config{
		Host:     config.Database.Host,
		Port:     config.Database.Port,
		Name:     config.Database.Name,
		User:     config.Database.User,
		Password: config.Database.Password,
		SSLMode:  config.Database.SSLMode,
	}
	db, err := database.NewPostgreSQL(dbConfig)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Initialize subscription service
	subscriptionService := billing.NewSubscriptionService(db, logger, &config.Subscription)

	// Determine target date
	var targetDate time.Time
	if len(os.Args) >= 3 {
		var err error
		targetDate, err = time.Parse("2006-01-02", os.Args[2])
		if err != nil {
			log.Fatalf("Invalid date format. Use YYYY-MM-DD: %v", err)
		}
	} else {
		targetDate = time.Now()
	}

	fmt.Printf("Processing monthly charges for %s...\n", targetDate.Format("2006-01-02"))

	// Process charges
	err = subscriptionService.ProcessMonthlyCharges(targetDate)
	if err != nil {
		log.Fatalf("Failed to process monthly charges: %v", err)
	}

	fmt.Println("✓ Monthly charges processed successfully")
}

func historyCommand() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: subscription-processor history <account_id>")
		os.Exit(1)
	}

	accountID := os.Args[2]
	logger := createLogger()
	config := loadConfig()

	// Initialize database
	dbConfig := database.Config{
		Host:     config.Database.Host,
		Port:     config.Database.Port,
		Name:     config.Database.Name,
		User:     config.Database.User,
		Password: config.Database.Password,
		SSLMode:  config.Database.SSLMode,
	}
	db, err := database.NewPostgreSQL(dbConfig)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Initialize subscription service
	subscriptionService := billing.NewSubscriptionService(db, logger, &config.Subscription)

	// Parse account ID
	var accountIDInt int
	fmt.Sscanf(accountID, "%d", &accountIDInt)

	// Get charge history
	charges, err := subscriptionService.GetAccountChargeHistory(accountIDInt, 20)
	if err != nil {
		log.Fatalf("Failed to get charge history: %v", err)
	}

	fmt.Printf("\nCharge history for account %s:\n", accountID)
	fmt.Println("=====================================")

	if len(charges) == 0 {
		fmt.Println("No charges found for this account")
		return
	}

	for _, charge := range charges {
		fmt.Printf("%s - $%.2f (%s)\n",
			charge.ChargeDate.Format("2006-01-02 15:04:05"),
			charge.Amount,
			charge.Status)
	}

	fmt.Printf("\nTotal charges: %d\n", len(charges))
}

func statsCommand() {
	config := loadConfig()

	// Initialize database
	dbConfig := database.Config{
		Host:     config.Database.Host,
		Port:     config.Database.Port,
		Name:     config.Database.Name,
		User:     config.Database.User,
		Password: config.Database.Password,
		SSLMode:  config.Database.SSLMode,
	}
	db, err := database.NewPostgreSQL(dbConfig)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Get statistics from database
	stats, err := getSubscriptionStats(db)
	if err != nil {
		log.Fatalf("Failed to get statistics: %v", err)
	}

	fmt.Println("\nSubscription Billing Statistics:")
	fmt.Println("=================================")
	fmt.Printf("Total Accounts: %d\n", stats.TotalAccounts)
	fmt.Printf("Active Accounts: %d\n", stats.ActiveAccounts)
	fmt.Printf("Charges This Month: %d\n", stats.ChargesThisMonth)
	fmt.Printf("Failed Charges: %d\n", stats.FailedCharges)
	fmt.Printf("Total Revenue: $%.2f\n", stats.TotalRevenue)
	fmt.Printf("Success Rate: %.1f%%\n", stats.SuccessRate)
}

func createLogger() *zap.Logger {
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}
	return logger
}

func loadConfig() *Config {
	configFile := "config.yaml"
	if len(os.Args) >= 3 && os.Args[len(os.Args)-2] == "--config" {
		configFile = os.Args[len(os.Args)-1]
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		log.Fatalf("Failed to parse config file: %v", err)
	}

	return &config
}

// SubscriptionStats статистика подписок
type SubscriptionStats struct {
	TotalAccounts    int     `json:"total_accounts"`
	ActiveAccounts   int     `json:"active_accounts"`
	ChargesThisMonth int     `json:"charges_this_month"`
	FailedCharges    int     `json:"failed_charges"`
	TotalRevenue     float64 `json:"total_revenue"`
	SuccessRate      float64 `json:"success_rate"`
}

func getSubscriptionStats(db *database.PostgreSQL) (*SubscriptionStats, error) {
	stats := &SubscriptionStats{}

	// Total accounts
	err := db.GetDB().QueryRow("SELECT COUNT(*) FROM accounts").Scan(&stats.TotalAccounts)
	if err != nil {
		return nil, err
	}

	// Active accounts
	err = db.GetDB().QueryRow("SELECT COUNT(*) FROM accounts WHERE active = true").Scan(&stats.ActiveAccounts)
	if err != nil {
		return nil, err
	}

	// Charges this month
	err = db.GetDB().QueryRow(`
		SELECT COUNT(*) FROM fin_transactions ft
		WHERE ft.comment LIKE 'Monthly subscription fee%'
		AND ft.created_at >= date_trunc('month', CURRENT_DATE)
		AND ft.amount < 0`).Scan(&stats.ChargesThisMonth)
	if err != nil {
		return nil, err
	}

	// Total revenue this month
	err = db.GetDB().QueryRow(`
		SELECT COALESCE(SUM(ABS(ft.amount)), 0) FROM fin_transactions ft
		WHERE ft.comment LIKE 'Monthly subscription fee%'
		AND ft.created_at >= date_trunc('month', CURRENT_DATE)
		AND ft.amount < 0`).Scan(&stats.TotalRevenue)
	if err != nil {
		return nil, err
	}

	// Success rate calculation
	if stats.ChargesThisMonth > 0 {
		stats.SuccessRate = float64(stats.ChargesThisMonth-stats.FailedCharges) / float64(stats.ChargesThisMonth) * 100
	}

	return stats, nil
}
