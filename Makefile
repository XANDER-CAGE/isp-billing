.PHONY: build test clean run validate-db docker

# Variables
APP_NAME = netspire-go
BUILD_DIR = build
CONFIG_FILE = config.yaml

# Build application
build:
	@echo "Building $(APP_NAME)..."
	go mod download
	go build -o $(BUILD_DIR)/$(APP_NAME) ./cmd/$(APP_NAME)
	@echo "✅ Build complete: $(BUILD_DIR)/$(APP_NAME)"

# Build validation tool
build-validate:
	@echo "Building database validation tool..."
	go build -o $(BUILD_DIR)/validate-db ./cmd/validate-db
	@echo "✅ Validation tool built: $(BUILD_DIR)/validate-db"

# Run application
run: build
	@echo "Starting $(APP_NAME)..."
	./$(BUILD_DIR)/$(APP_NAME) --config $(CONFIG_FILE)

# Validate database schema compatibility
validate-db: build-validate
	@echo "🔍 Validating database schema..."
	./$(BUILD_DIR)/validate-db $(CONFIG_FILE)

# Test database connection
test-db:
	@echo "🔌 Testing database connection..."
	go run cmd/validate-db/main.go $(CONFIG_FILE)

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	go clean

# Install dependencies
deps:
	@echo "Installing dependencies..."
	go mod download
	go mod tidy

# Run linter
lint:
	@echo "Running linter..."
	golangci-lint run

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Development mode (with hot reload)
dev:
	@echo "Starting development mode..."
	air -c .air.toml

# Build Docker image
docker:
	@echo "Building Docker image..."
	docker build -t $(APP_NAME):latest .

# Deploy to staging
deploy-staging: build
	@echo "Deploying to staging..."
	systemctl stop netspire-go || true
	cp $(BUILD_DIR)/$(APP_NAME) /usr/local/bin/
	cp $(CONFIG_FILE) /etc/netspire-go/
	systemctl start netspire-go
	systemctl status netspire-go

# Deploy to production (с проверками)
deploy-prod: validate-db build
	@echo "🚀 Deploying to production..."
	@echo "⚠️  This will replace the Erlang Netspire system!"
	@read -p "Are you sure? (yes/no): " confirm && [ "$$confirm" = "yes" ]
	@echo "Creating backup..."
	pg_dump netspire > /backup/netspire_backup_$$(date +%Y%m%d_%H%M%S).sql
	@echo "Stopping Erlang Netspire..."
	systemctl stop netspire || true
	@echo "Installing Go Netspire..."
	cp $(BUILD_DIR)/$(APP_NAME) /usr/local/bin/
	cp $(CONFIG_FILE) /etc/netspire-go/
	systemctl start netspire-go
	@echo "Checking health..."
	sleep 5
	curl -f http://localhost:8080/health || (echo "❌ Health check failed!" && exit 1)
	@echo "✅ Production deployment successful!"

# Rollback to Erlang system
rollback:
	@echo "🔄 Rolling back to Erlang Netspire..."
	systemctl stop netspire-go || true
	systemctl start netspire
	systemctl status netspire
	@echo "✅ Rollback complete"

# Show help
help:
	@echo "Available commands:"
	@echo "  build          - Build the application"
	@echo "  run            - Build and run the application"
	@echo "  validate-db    - Validate database schema compatibility"
	@echo "  test-db        - Test database connection"
	@echo "  test           - Run tests"
	@echo "  clean          - Clean build artifacts"
	@echo "  deps           - Install dependencies"
	@echo "  lint           - Run linter"
	@echo "  fmt            - Format code"
	@echo "  dev            - Development mode with hot reload"
	@echo "  docker         - Build Docker image"
	@echo "  deploy-staging - Deploy to staging"
	@echo "  deploy-prod    - Deploy to production (with safety checks)"
	@echo "  rollback       - Rollback to Erlang system"
	@echo ""
	@echo "📚 Documentation:"
	@echo "  docs           - View all documentation"
	@echo "  analysis       - View modules analysis"

# Documentation commands
docs:
	@echo "📚 NETSPIRE-GO DOCUMENTATION"
	@echo "=============================="
	@echo ""
	@echo "📖 QUICK_START.md           - Быстрый запуск и тестирование"
	@echo "🔍 MISSING_FEATURES.md      - Анализ недостающей функциональности"
	@echo "🏗️ MISSING_MODULES_ANALYSIS.md - Детальный анализ всех модулей"
	@echo "📊 MODULES_COMPARISON_SUMMARY.md - Итоговое сравнение Erlang vs Go"
	@echo ""
	@echo "💡 Для просмотра конкретного файла: cat FILENAME.md"

analysis:
	@echo "🔍 АНАЛИЗ НЕДОСТАЮЩИХ МОДУЛЕЙ"
	@echo "================================"
	@echo ""
	@echo "📊 Общий статус:"
	@echo "  ✅ Готово к тестированию: 85%"
	@echo "  ⚠️  Критические недостатки: 3 модуля"
	@echo "  🚀 До production: 7-11 дней"
	@echo ""
	@echo "🚨 Критически важные недостающие модули:"
	@echo "  1. IP Pool Management (mod_ippool.erl)"
	@echo "  2. Session Memory Store (iptraffic_session.erl)" 
	@echo "  3. Disconnect Mechanisms (mod_disconnect_*.erl)"
	@echo ""
	@echo "📋 Подробности: cat MODULES_COMPARISON_SUMMARY.md" 