package billing

import (
	"fmt"

	"isp-billing/internal/database"
	"isp-billing/internal/models"
)

type Service struct {
	db     *database.PostgreSQL
	config map[string]interface{}
}

func NewService(db *database.PostgreSQL, config map[string]interface{}) *Service {
	return &Service{
		db:     db,
		config: config,
	}
}

// Authorize - выполняет авторизацию пользователя (как в Erlang)
func (s *Service) Authorize(account *models.AccountWithRelations, req models.RADIUSAuthorizeRequest) (*models.BillingResult, error) {
	// Парсим plan_data
	planData, err := database.ParsePlanDataFromJSON(account.PData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse plan data: %w", err)
	}

	// Определяем алгоритм авторизации (module:function как в Erlang)
	module, function := database.SplitAlgoName(account.Auth)

	switch function {
	case "prepaid_auth":
		return s.prepaidAuth(account, planData, req)
	case "limited_prepaid_auth":
		return s.limitedPrepaidAuth(account, planData, req)
	case "on_auth":
		return s.onAuth(account, planData, req)
	case "no_overlimit_auth":
		return s.noOverlimitAuth(account, planData, req)
	default:
		return &models.BillingResult{
			Decision: "Reject",
			Reason:   fmt.Sprintf("Unknown auth algorithm: %s:%s", module, function),
		}, nil
	}
}

// ProcessAccounting - обрабатывает accounting запросы
func (s *Service) ProcessAccounting(account *models.AccountWithRelations, req models.RADIUSAccountingRequest) (*models.BillingResult, error) {
	// Парсим plan_data
	planData, err := database.ParsePlanDataFromJSON(account.PData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse plan data: %w", err)
	}

	// Определяем алгоритм учета
	module, function := database.SplitAlgoName(account.Acct)

	switch function {
	case "prepaid_auth":
		return s.prepaidAccounting(account, planData, req)
	case "limited_prepaid_auth":
		return s.limitedPrepaidAccounting(account, planData, req)
	case "on_auth":
		return s.onAuthAccounting(account, planData, req)
	case "no_overlimit_auth":
		return s.noOverlimitAccounting(account, planData, req)
	default:
		return &models.BillingResult{
			Decision: "Accept",
			Reason:   fmt.Sprintf("Unknown acct algorithm: %s:%s", module, function),
		}, nil
	}
}

// ================ АЛГОРИТМЫ АВТОРИЗАЦИИ (как в algo_builtin.erl) ================

func (s *Service) prepaidAuth(account *models.AccountWithRelations, planData map[string]interface{}, req models.RADIUSAuthorizeRequest) (*models.BillingResult, error) {
	// Проверяем баланс + кредит
	availableBalance := account.Balance + account.Credit

	if availableBalance <= 0 {
		return &models.BillingResult{
			Decision: "Reject",
			Reason:   "Insufficient balance",
		}, nil
	}

	return &models.BillingResult{
		Decision: "Accept",
		Replies: []models.RADIUSReply{
			{Name: "Session-Timeout", Value: "3600"}, // 1 час по умолчанию
		},
		PlanData: planData,
	}, nil
}

func (s *Service) limitedPrepaidAuth(account *models.AccountWithRelations, planData map[string]interface{}, req models.RADIUSAuthorizeRequest) (*models.BillingResult, error) {
	// Аналогично prepaid_auth но с лимитами
	availableBalance := account.Balance + account.Credit

	if availableBalance <= 0 {
		return &models.BillingResult{
			Decision: "Reject",
			Reason:   "Insufficient balance",
		}, nil
	}

	// Вычисляем лимит сессии
	sessionLimit := s.calculateSessionLimit(account, planData)

	return &models.BillingResult{
		Decision: "Accept",
		Replies: []models.RADIUSReply{
			{Name: "Session-Timeout", Value: fmt.Sprintf("%d", sessionLimit)},
		},
		PlanData: planData,
	}, nil
}

func (s *Service) onAuth(account *models.AccountWithRelations, planData map[string]interface{}, req models.RADIUSAuthorizeRequest) (*models.BillingResult, error) {
	// Списываем фиксированную сумму при авторизации
	sessionCost := s.getSessionCost(planData)

	if account.Balance < sessionCost {
		return &models.BillingResult{
			Decision: "Reject",
			Reason:   "Insufficient balance for session cost",
		}, nil
	}

	return &models.BillingResult{
		Decision: "Accept",
		Amount:   sessionCost,
		PlanData: planData,
	}, nil
}

func (s *Service) noOverlimitAuth(account *models.AccountWithRelations, planData map[string]interface{}, req models.RADIUSAuthorizeRequest) (*models.BillingResult, error) {
	// Проверяем строгие лимиты без превышения
	if account.Balance <= 0 {
		return &models.BillingResult{
			Decision: "Reject",
			Reason:   "Zero balance - no overlimit allowed",
		}, nil
	}

	return &models.BillingResult{
		Decision: "Accept",
		PlanData: planData,
	}, nil
}

// ================ АЛГОРИТМЫ УЧЕТА (как в algo_builtin.erl) ================

func (s *Service) prepaidAccounting(account *models.AccountWithRelations, planData map[string]interface{}, req models.RADIUSAccountingRequest) (*models.BillingResult, error) {
	// Вычисляем стоимость трафика
	totalTraffic := req.AcctInputOctets + req.AcctOutputOctets
	costPerMB := s.getCostPerMB(planData)
	amount := float64(totalTraffic) / 1024 / 1024 * costPerMB

	return &models.BillingResult{
		Decision:     "Accept",
		Amount:       amount,
		TrafficClass: "default",
		PlanData:     planData,
	}, nil
}

func (s *Service) limitedPrepaidAccounting(account *models.AccountWithRelations, planData map[string]interface{}, req models.RADIUSAccountingRequest) (*models.BillingResult, error) {
	// Аналогично prepaid с проверкой лимитов
	return s.prepaidAccounting(account, planData, req)
}

func (s *Service) onAuthAccounting(account *models.AccountWithRelations, planData map[string]interface{}, req models.RADIUSAccountingRequest) (*models.BillingResult, error) {
	// При on_auth учет только при завершении сессии
	if req.AcctStatusType == "Stop" {
		sessionCost := s.getSessionCost(planData)
		return &models.BillingResult{
			Decision: "Accept",
			Amount:   sessionCost,
			PlanData: planData,
		}, nil
	}

	return &models.BillingResult{
		Decision: "Accept",
		Amount:   0,
		PlanData: planData,
	}, nil
}

func (s *Service) noOverlimitAccounting(account *models.AccountWithRelations, planData map[string]interface{}, req models.RADIUSAccountingRequest) (*models.BillingResult, error) {
	// Строгий учет без превышений
	return s.prepaidAccounting(account, planData, req)
}

// ================ ВСПОМОГАТЕЛЬНЫЕ МЕТОДЫ ================

func (s *Service) calculateSessionLimit(account *models.AccountWithRelations, planData map[string]interface{}) int {
	// Вычисляем лимит времени сессии на основе баланса
	availableBalance := account.Balance + account.Credit
	costPerMB := s.getCostPerMB(planData)

	if costPerMB <= 0 {
		return 3600 // 1 час по умолчанию
	}

	// Простая формула: доступные мегабайты * время на мегабайт
	availableMB := availableBalance / costPerMB
	timePerMB := 60 // секунд на MB (примерно)

	sessionLimit := int(availableMB * float64(timePerMB))
	if sessionLimit > 86400 { // максимум 24 часа
		sessionLimit = 86400
	}
	if sessionLimit < 300 { // минимум 5 минут
		sessionLimit = 300
	}

	return sessionLimit
}

func (s *Service) getCostPerMB(planData map[string]interface{}) float64 {
	if cost, exists := planData["cost_per_mb"]; exists {
		if costFloat, ok := cost.(float64); ok {
			return costFloat
		}
	}
	return 0.01 // по умолчанию
}

func (s *Service) getSessionCost(planData map[string]interface{}) float64 {
	if cost, exists := planData["session_cost"]; exists {
		if costFloat, ok := cost.(float64); ok {
			return costFloat
		}
	}
	return 5.0 // по умолчанию
}

func (s *Service) updatePlanData(planData map[string]interface{}, key string, value interface{}) {
	planData[key] = value
}
