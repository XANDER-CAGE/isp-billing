package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/lib/pq"
	"gopkg.in/yaml.v2"
)

type Config struct {
	Database struct {
		Host     string `yaml:"host"`
		Port     int    `yaml:"port"`
		Name     string `yaml:"name"`
		User     string `yaml:"user"`
		Password string `yaml:"password"`
		SSLMode  string `yaml:"sslmode"`
	} `yaml:"database"`
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: go run main.go <config.yaml>")
	}

	// Load config
	configData, err := os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatalf("Failed to read config: %v", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(configData, &cfg); err != nil {
		log.Fatalf("Failed to parse config: %v", err)
	}

	// Connect to database
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Database.Host, cfg.Database.Port, cfg.Database.User,
		cfg.Database.Password, cfg.Database.Name, cfg.Database.SSLMode)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}

	fmt.Println("✅ Database connection successful")

	// Validate schema
	if err := validateSchema(db); err != nil {
		log.Fatalf("❌ Schema validation failed: %v", err)
	}

	fmt.Println("✅ All schema validations passed")
	fmt.Println("🎉 Database is ready for Netspire Go migration!")
}

func validateSchema(db *sql.DB) error {
	// Check required tables
	requiredTables := []string{
		"currencies", "currencies_rate", "plans", "contract_kinds",
		"contracts", "fin_transactions", "accounts", "radius_replies",
		"assigned_radius_replies", "iptraffic_sessions", "session_details",
		"admins", "contract_info_items", "contract_info",
	}

	fmt.Println("\n🔍 Validating required tables...")
	for _, table := range requiredTables {
		if err := checkTable(db, table); err != nil {
			return fmt.Errorf("table %s: %w", table, err)
		}
		fmt.Printf("  ✅ %s\n", table)
	}

	// Check required columns in critical tables
	fmt.Println("\n🔍 Validating critical table structures...")

	// accounts table
	accountsCols := map[string]string{
		"id": "integer", "login": "varchar", "password": "varchar",
		"active": "boolean", "plan_id": "integer", "contract_id": "integer",
		"plan_data": "varchar", "created_at": "timestamp",
	}
	if err := checkColumns(db, "accounts", accountsCols); err != nil {
		return fmt.Errorf("accounts table structure: %w", err)
	}
	fmt.Println("  ✅ accounts table structure")

	// iptraffic_sessions table
	sessionsCols := map[string]string{
		"id": "integer", "account_id": "integer", "sid": "varchar",
		"cid": "varchar", "ip": "varchar", "octets_in": "bigint",
		"octets_out": "bigint", "amount": "numeric", "started_at": "timestamp",
		"updated_at": "timestamp", "finished_at": "timestamp", "expired": "boolean",
	}
	if err := checkColumns(db, "iptraffic_sessions", sessionsCols); err != nil {
		return fmt.Errorf("iptraffic_sessions table structure: %w", err)
	}
	fmt.Println("  ✅ iptraffic_sessions table structure")

	// Check required functions
	fmt.Println("\n🔍 Validating PostgreSQL functions...")
	requiredFunctions := []string{
		"make_transaction", "credit_transaction", "debit_transaction",
	}
	for _, function := range requiredFunctions {
		if err := checkFunction(db, function); err != nil {
			return fmt.Errorf("function %s: %w", function, err)
		}
		fmt.Printf("  ✅ %s()\n", function)
	}

	// Check data integrity
	fmt.Println("\n🔍 Validating data integrity...")

	// Check for orphaned sessions
	var orphanedSessions int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM iptraffic_sessions s 
		LEFT JOIN accounts a ON s.account_id = a.id 
		WHERE a.id IS NULL`).Scan(&orphanedSessions)
	if err != nil {
		return fmt.Errorf("checking orphaned sessions: %w", err)
	}
	if orphanedSessions > 0 {
		fmt.Printf("  ⚠️  Found %d orphaned sessions (sessions without accounts)\n", orphanedSessions)
	} else {
		fmt.Println("  ✅ No orphaned sessions")
	}

	// Check for invalid plan_data JSON
	var invalidPlanData int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM accounts 
		WHERE plan_data != '' AND plan_data !~ '^[\s]*[{\[]'`).Scan(&invalidPlanData)
	if err != nil {
		return fmt.Errorf("checking plan_data format: %w", err)
	}
	if invalidPlanData > 0 {
		fmt.Printf("  ⚠️  Found %d accounts with invalid plan_data JSON format\n", invalidPlanData)
	} else {
		fmt.Println("  ✅ All plan_data fields have valid JSON format")
	}

	// Show some statistics
	fmt.Println("\n📊 Database statistics:")
	stats := map[string]string{
		"Total accounts":  "SELECT COUNT(*) FROM accounts",
		"Active accounts": "SELECT COUNT(*) FROM accounts WHERE active = true",
		"Active sessions": "SELECT COUNT(*) FROM iptraffic_sessions WHERE finished_at IS NULL",
		"Total plans":     "SELECT COUNT(*) FROM plans",
		"Total contracts": "SELECT COUNT(*) FROM contracts",
	}

	for name, query := range stats {
		var count int
		if err := db.QueryRow(query).Scan(&count); err != nil {
			fmt.Printf("  ❓ %s: error getting count\n", name)
		} else {
			fmt.Printf("  📈 %s: %d\n", name, count)
		}
	}

	return nil
}

func checkTable(db *sql.DB, tableName string) error {
	var exists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_schema = 'public' 
			AND table_name = $1
		)`, tableName).Scan(&exists)

	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}

	if !exists {
		return fmt.Errorf("table does not exist")
	}

	return nil
}

func checkColumns(db *sql.DB, tableName string, requiredCols map[string]string) error {
	for colName, expectedType := range requiredCols {
		var dataType string
		err := db.QueryRow(`
			SELECT data_type FROM information_schema.columns 
			WHERE table_schema = 'public' 
			AND table_name = $1 
			AND column_name = $2`, tableName, colName).Scan(&dataType)

		if err == sql.ErrNoRows {
			return fmt.Errorf("column %s does not exist", colName)
		}
		if err != nil {
			return fmt.Errorf("query error for column %s: %w", colName, err)
		}

		// Simple type mapping check
		if !isCompatibleType(dataType, expectedType) {
			return fmt.Errorf("column %s has type %s, expected compatible with %s",
				colName, dataType, expectedType)
		}
	}
	return nil
}

func checkFunction(db *sql.DB, functionName string) error {
	var exists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT FROM information_schema.routines 
			WHERE routine_schema = 'public' 
			AND routine_name = $1
			AND routine_type = 'FUNCTION'
		)`, functionName).Scan(&exists)

	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}

	if !exists {
		return fmt.Errorf("function does not exist")
	}

	return nil
}

func isCompatibleType(actual, expected string) bool {
	// Simple type compatibility check
	compatible := map[string][]string{
		"integer":   {"integer", "bigint", "int", "int4", "int8"},
		"varchar":   {"character varying", "varchar", "text", "character"},
		"boolean":   {"boolean", "bool"},
		"numeric":   {"numeric", "decimal", "money"},
		"bigint":    {"bigint", "integer", "int8", "int4"},
		"timestamp": {"timestamp without time zone", "timestamp", "timestamptz"},
	}

	if expectedTypes, exists := compatible[expected]; exists {
		for _, validType := range expectedTypes {
			if actual == validType {
				return true
			}
		}
	}

	return actual == expected
}
