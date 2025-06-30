package billing

import (
	"fmt"
	"time"

	"netspire-go/internal/database"
	"netspire-go/internal/models"

	"go.uber.org/zap"
)

// SubscriptionService handles automatic subscription fee charges
// Новая функциональность для автоматических списаний абонентской платы
type SubscriptionService struct {
	db     *database.PostgreSQL
	logger *zap.Logger
	config *SubscriptionConfig
}

// SubscriptionConfig configuration for subscription billing
type SubscriptionConfig struct {
	Enabled                    bool    `yaml:"enabled"`
	DefaultMonthlyFee          float64 `yaml:"default_monthly_fee"`
	GracePeriodDays            int     `yaml:"grace_period_days"`
	DisableOnInsufficientFunds bool    `yaml:"disable_on_insufficient_funds"`
	ProcessingTime             string  `yaml:"processing_time"` // "02:00" - время обработки
	EnableProration            bool    `yaml:"enable_proration"`
}

// SubscriptionCharge represents a subscription charge record
type SubscriptionCharge struct {
	AccountID     int       `json:"account_id"`
	PlanID        int       `json:"plan_id"`
	Amount        float64   `json:"amount"`
	ChargeDate    time.Time `json:"charge_date"`
	PeriodStart   time.Time `json:"period_start"`
	PeriodEnd     time.Time `json:"period_end"`
	Status        string    `json:"status"` // "success", "failed", "pending"
	FailureReason string    `json:"failure_reason,omitempty"`
	TransactionID *int      `json:"transaction_id,omitempty"`
}

// NewSubscriptionService creates a new subscription service
func NewSubscriptionService(db *database.PostgreSQL, logger *zap.Logger, config *SubscriptionConfig) *SubscriptionService {
	return &SubscriptionService{
		db:     db,
		logger: logger,
		config: config,
	}
}

// ProcessMonthlyCharges processes monthly subscription charges for all active accounts
// Основная функция для ежемесячных списаний
func (s *SubscriptionService) ProcessMonthlyCharges(targetDate time.Time) error {
	s.logger.Info("Starting monthly subscription charges processing",
		zap.Time("target_date", targetDate))

	// Получаем всех активных пользователей
	accounts, err := s.getActiveAccountsForBilling(targetDate)
	if err != nil {
		return fmt.Errorf("failed to get active accounts: %w", err)
	}

	s.logger.Info("Found accounts for billing", zap.Int("count", len(accounts)))

	successCount := 0
	failureCount := 0

	for _, account := range accounts {
		charge, err := s.processAccountCharge(account, targetDate)
		if err != nil {
			s.logger.Error("Failed to process account charge",
				zap.Int("account_id", account.ID),
				zap.String("login", account.Login),
				zap.Error(err))
			failureCount++
			continue
		}

		if charge.Status == "success" {
			successCount++
		} else {
			failureCount++
		}

		s.logger.Info("Processed account charge",
			zap.Int("account_id", account.ID),
			zap.String("login", account.Login),
			zap.String("status", charge.Status),
			zap.Float64("amount", charge.Amount))
	}

	s.logger.Info("Monthly charges processing completed",
		zap.Int("success", successCount),
		zap.Int("failures", failureCount),
		zap.Int("total", len(accounts)))

	return nil
}

// processAccountCharge processes subscription charge for single account
func (s *SubscriptionService) processAccountCharge(account *models.AccountWithSubscription, targetDate time.Time) (*SubscriptionCharge, error) {
	// Parse plan data
	planData, err := database.ParsePlanDataFromJSON(account.PData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse plan data: %w", err)
	}

	// Get subscription fee from plan data or use default
	monthlyFee := s.getMonthlyFee(planData)
	if monthlyFee <= 0 {
		// No subscription fee for this account
		return &SubscriptionCharge{
			AccountID:  account.ID,
			PlanID:     account.PId,
			Amount:     0,
			ChargeDate: targetDate,
			Status:     "success",
		}, nil
	}

	// Calculate billing period
	periodStart, periodEnd := s.calculateBillingPeriod(targetDate)

	// Check if already charged for this period
	alreadyCharged, err := s.isAlreadyCharged(account.ID, periodStart, periodEnd)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing charges: %w", err)
	}

	if alreadyCharged {
		s.logger.Debug("Account already charged for this period",
			zap.Int("account_id", account.ID))
		return &SubscriptionCharge{
			AccountID: account.ID,
			Status:    "success", // Already processed
		}, nil
	}

	// Apply proration if enabled and account is new
	finalAmount := monthlyFee
	if s.config.EnableProration {
		finalAmount = s.calculateProratedAmount(monthlyFee, account.CreatedAt, periodStart, periodEnd)
	}

	// Create charge record
	charge := &SubscriptionCharge{
		AccountID:   account.ID,
		PlanID:      account.PId,
		Amount:      finalAmount,
		ChargeDate:  time.Now(),
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		Status:      "pending",
	}

	// Check if account has sufficient balance (including credit)
	availableBalance := account.Balance + account.Credit
	if availableBalance < finalAmount {
		charge.Status = "failed"
		charge.FailureReason = "insufficient_funds"

		// Disable account if configured
		if s.config.DisableOnInsufficientFunds {
			err = s.disableAccount(account.ID)
			if err != nil {
				s.logger.Error("Failed to disable account",
					zap.Int("account_id", account.ID),
					zap.Error(err))
			}
		}

		return charge, nil
	}

	// Perform debit transaction
	comment := fmt.Sprintf("Monthly subscription fee for period %s - %s",
		periodStart.Format("2006-01-02"),
		periodEnd.Format("2006-01-02"))

	var newBalance float64
	err = s.db.GetDB().QueryRow(models.DebitTransactionQuery,
		account.ID, finalAmount, comment, nil).Scan(&newBalance)
	if err != nil {
		charge.Status = "failed"
		charge.FailureReason = fmt.Sprintf("transaction_failed: %v", err)
		return charge, nil
	}

	// Update charge record
	charge.Status = "success"

	// Save charge record to database
	err = s.saveChargeRecord(charge)
	if err != nil {
		s.logger.Error("Failed to save charge record",
			zap.Int("account_id", account.ID),
			zap.Error(err))
	}

	return charge, nil
}

// getActiveAccountsForBilling gets all active accounts that need billing
func (s *SubscriptionService) getActiveAccountsForBilling(targetDate time.Time) ([]*models.AccountWithSubscription, error) {
	query := `
		SELECT a.id, a.login, a.plan_data, a.plan_id, a.created_at,
			p.auth_algo, p.acct_algo, c.balance, c.currency_id, 
			COALESCE(sp.credit, 0.0) as credit
		FROM accounts a 
		LEFT OUTER JOIN service_params sp ON a.id=sp.account_id
		JOIN plans p ON a.plan_id = p.id
		JOIN contracts c ON a.contract_id = c.id
		WHERE a.active = true
		ORDER BY a.id`

	rows, err := s.db.GetDB().Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []*models.AccountWithSubscription
	for rows.Next() {
		account := &models.AccountWithSubscription{}
		err := rows.Scan(
			&account.ID,
			&account.Login,
			&account.PData,
			&account.PId,
			&account.CreatedAt,
			&account.Auth,
			&account.Acct,
			&account.Balance,
			&account.Currency,
			&account.Credit,
		)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}

	return accounts, rows.Err()
}

// getMonthlyFee extracts monthly fee from plan data
func (s *SubscriptionService) getMonthlyFee(planData map[string]interface{}) float64 {
	// Check for monthly_fee in plan data
	if fee, exists := planData["MONTHLY_FEE"]; exists {
		if feeFloat, ok := fee.(float64); ok {
			return feeFloat
		}
		if feeInt, ok := fee.(int); ok {
			return float64(feeInt)
		}
	}

	// Check for subscription_fee
	if fee, exists := planData["SUBSCRIPTION_FEE"]; exists {
		if feeFloat, ok := fee.(float64); ok {
			return feeFloat
		}
	}

	// Use default from config
	return s.config.DefaultMonthlyFee
}

// calculateBillingPeriod calculates billing period for given date
func (s *SubscriptionService) calculateBillingPeriod(targetDate time.Time) (time.Time, time.Time) {
	// Start of the month
	periodStart := time.Date(targetDate.Year(), targetDate.Month(), 1, 0, 0, 0, 0, targetDate.Location())

	// End of the month
	periodEnd := periodStart.AddDate(0, 1, 0).Add(-time.Second)

	return periodStart, periodEnd
}

// calculateProratedAmount calculates prorated amount for partial month
func (s *SubscriptionService) calculateProratedAmount(monthlyFee float64, accountCreated, periodStart, periodEnd time.Time) float64 {
	// If account was created before billing period, charge full amount
	if accountCreated.Before(periodStart) {
		return monthlyFee
	}

	// If account was created after billing period, no charge
	if accountCreated.After(periodEnd) {
		return 0
	}

	// Calculate proration
	totalDays := periodEnd.Sub(periodStart).Hours() / 24
	remainingDays := periodEnd.Sub(accountCreated).Hours() / 24

	if remainingDays <= 0 {
		return 0
	}

	proratedAmount := monthlyFee * (remainingDays / totalDays)
	return proratedAmount
}

// isAlreadyCharged checks if account was already charged for the period
func (s *SubscriptionService) isAlreadyCharged(accountID int, periodStart, periodEnd time.Time) (bool, error) {
	// Check fin_transactions for subscription charges
	var count int
	err := s.db.GetDB().QueryRow(`
		SELECT COUNT(*) FROM fin_transactions ft
		JOIN accounts a ON ft.contract_id = (SELECT contract_id FROM accounts WHERE id = $1)
		WHERE ft.comment LIKE 'Monthly subscription fee%'
		AND ft.created_at >= $2 AND ft.created_at <= $3
		AND ft.amount < 0`, // Debit transactions
		accountID, periodStart, periodEnd).Scan(&count)

	return count > 0, err
}

// saveChargeRecord saves charge record to custom table (optional)
func (s *SubscriptionService) saveChargeRecord(charge *SubscriptionCharge) error {
	// This would save to a subscription_charges table if it exists
	// For now, we rely on fin_transactions table
	return nil
}

// disableAccount disables account due to insufficient funds
func (s *SubscriptionService) disableAccount(accountID int) error {
	_, err := s.db.GetDB().Exec(`UPDATE accounts SET active = false WHERE id = $1`, accountID)
	return err
}

// GetAccountChargeHistory returns charge history for account
func (s *SubscriptionService) GetAccountChargeHistory(accountID int, limit int) ([]*SubscriptionCharge, error) {
	query := `
		SELECT ft.amount, ft.created_at, ft.comment, ft.balance_after
		FROM fin_transactions ft
		JOIN accounts a ON ft.contract_id = (SELECT contract_id FROM accounts WHERE id = $1)
		WHERE ft.comment LIKE 'Monthly subscription fee%'
		AND ft.amount < 0
		ORDER BY ft.created_at DESC
		LIMIT $2`

	rows, err := s.db.GetDB().Query(query, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var charges []*SubscriptionCharge
	for rows.Next() {
		charge := &SubscriptionCharge{}
		var comment string
		var balanceAfter float64

		err := rows.Scan(&charge.Amount, &charge.ChargeDate, &comment, &balanceAfter)
		if err != nil {
			return nil, err
		}

		charge.AccountID = accountID
		charge.Amount = -charge.Amount // Convert to positive
		charge.Status = "success"

		charges = append(charges, charge)
	}

	return charges, rows.Err()
}

// ScheduledProcessor handles scheduled execution of monthly charges
type ScheduledProcessor struct {
	service *SubscriptionService
	logger  *zap.Logger
}

// NewScheduledProcessor creates a new scheduled processor
func NewScheduledProcessor(service *SubscriptionService, logger *zap.Logger) *ScheduledProcessor {
	return &ScheduledProcessor{
		service: service,
		logger:  logger,
	}
}

// RunMonthlyCharges runs monthly charges for specified date or current date
func (p *ScheduledProcessor) RunMonthlyCharges(targetDate *time.Time) error {
	var processDate time.Time
	if targetDate != nil {
		processDate = *targetDate
	} else {
		processDate = time.Now()
	}

	p.logger.Info("Running scheduled monthly charges", zap.Time("date", processDate))

	err := p.service.ProcessMonthlyCharges(processDate)
	if err != nil {
		p.logger.Error("Failed to process monthly charges", zap.Error(err))
		return err
	}

	return nil
}

// StartDailyScheduler starts daily scheduler for subscription charges
func (p *ScheduledProcessor) StartDailyScheduler() {
	go func() {
		for {
			now := time.Now()

			// Calculate next run time (configured processing time)
			processingTime := "02:00" // Default 2 AM
			if p.service.config.ProcessingTime != "" {
				processingTime = p.service.config.ProcessingTime
			}

			nextRun := p.getNextRunTime(now, processingTime)
			sleepDuration := nextRun.Sub(now)

			p.logger.Info("Subscription processor scheduled",
				zap.Time("next_run", nextRun),
				zap.Duration("sleep_duration", sleepDuration))

			time.Sleep(sleepDuration)

			// Check if it's the first day of the month
			if nextRun.Day() == 1 {
				err := p.RunMonthlyCharges(nil)
				if err != nil {
					p.logger.Error("Daily scheduled processing failed", zap.Error(err))
				}
			}
		}
	}()
}

// getNextRunTime calculates next run time based on processing time
func (p *ScheduledProcessor) getNextRunTime(now time.Time, processingTime string) time.Time {
	// Parse processing time
	var hour, minute int
	fmt.Sscanf(processingTime, "%d:%d", &hour, &minute)

	// Calculate next run time
	nextRun := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())

	// If time has passed today, schedule for tomorrow
	if nextRun.Before(now) {
		nextRun = nextRun.AddDate(0, 0, 1)
	}

	return nextRun
}
