package models

import (
	"encoding/json"
	"time"
)

// ================ ТОЧНЫЕ МОДЕЛИ СУЩЕСТВУЮЩЕЙ СХЕМЫ БД ================

// DBCurrency - таблица currencies
type DBCurrency struct {
	ID          int    `json:"id" db:"id"`
	ShortName   string `json:"short_name" db:"short_name"`
	Name        string `json:"name" db:"name"`
	Description string `json:"description" db:"description"`
}

// DBCurrencyRate - таблица currencies_rate
type DBCurrencyRate struct {
	FromID int     `json:"from_id" db:"from_id"`
	ToID   int     `json:"to_id" db:"to_id"`
	Rate   float64 `json:"rate" db:"rate"`
}

// DBPlan - таблица plans (ТОЧНОЕ СООТВЕТСТВИЕ СХЕМЕ)
type DBPlan struct {
	ID         int       `json:"id" db:"id"`
	Name       string    `json:"name" db:"name"`
	Code       string    `json:"code" db:"code"`
	CurrencyID int       `json:"currency_id" db:"currency_id"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time `json:"updated_at" db:"updated_at"`
	AuthAlgo   string    `json:"auth_algo" db:"auth_algo"`
	AcctAlgo   string    `json:"acct_algo" db:"acct_algo"`
	Settings   string    `json:"settings" db:"settings"`
}

// DBContractKind - таблица contract_kinds
type DBContractKind struct {
	ID          int    `json:"id" db:"id"`
	KindName    string `json:"kind_name" db:"kind_name"`
	Description string `json:"description" db:"description"`
}

// DBContract - таблица contracts (ТОЧНОЕ СООТВЕТСТВИЕ СХЕМЕ)
type DBContract struct {
	ID         int       `json:"id" db:"id"`
	KindID     int       `json:"kind_id" db:"kind_id"`
	Balance    float64   `json:"balance" db:"balance"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
	UpdatedAt  time.Time `json:"updated_at" db:"updated_at"`
	CurrencyID int       `json:"currency_id" db:"currency_id"`
}

// DBFinTransaction - таблица fin_transactions
type DBFinTransaction struct {
	ID                       int       `json:"id" db:"id"`
	KindID                   int       `json:"kind_id" db:"kind_id"`
	ContractID               int       `json:"contract_id" db:"contract_id"`
	CurrencyID               int       `json:"currency_id" db:"currency_id"`
	Amount                   float64   `json:"amount" db:"amount"`
	AmountInContractCurrency float64   `json:"amount_in_contract_currency" db:"amount_in_contract_currency"`
	CreatedAt                time.Time `json:"created_at" db:"created_at"`
	BalanceAfter             float64   `json:"balance_after" db:"balance_after"`
	Comment                  string    `json:"comment" db:"comment"`
}

// DBAccount - таблица accounts (ТОЧНОЕ СООТВЕТСТВИЕ СХЕМЕ)
type DBAccount struct {
	ID         int       `json:"id" db:"id"`
	ContractID int       `json:"contract_id" db:"contract_id"`
	PlanID     int       `json:"plan_id" db:"plan_id"`
	Login      string    `json:"login" db:"login"`
	Password   string    `json:"password" db:"password"`
	Active     bool      `json:"active" db:"active"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
	PlanData   string    `json:"plan_data" db:"plan_data"` // JSON as VARCHAR - НЕ ИЗМЕНЯЕМ!
}

// DBRadiusReply - таблица radius_replies
type DBRadiusReply struct {
	ID          int        `json:"id" db:"id"`
	Name        string     `json:"name" db:"name"`
	Description *string    `json:"description" db:"description"`
	Active      bool       `json:"active" db:"active"`
	CreatedAt   *time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   *time.Time `json:"updated_at" db:"updated_at"`
}

// DBAssignedRadiusReply - таблица assigned_radius_replies
type DBAssignedRadiusReply struct {
	ID            int        `json:"id" db:"id"`
	TargetID      int        `json:"target_id" db:"target_id"`
	TargetType    string     `json:"target_type" db:"target_type"`
	RadiusReplyID int        `json:"radius_reply_id" db:"radius_reply_id"`
	Value         string     `json:"value" db:"value"`
	CreatedAt     *time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     *time.Time `json:"updated_at" db:"updated_at"`
}

// DBIPTrafficSession - таблица iptraffic_sessions (ТОЧНОЕ СООТВЕТСТВИЕ СХЕМЕ)
type DBIPTrafficSession struct {
	ID         int        `json:"id" db:"id"`
	AccountID  int        `json:"account_id" db:"account_id"`
	SID        string     `json:"sid" db:"sid"`
	CID        *string    `json:"cid" db:"cid"` // VARCHAR(128) - может быть NULL
	IP         string     `json:"ip" db:"ip"`
	OctetsIn   int64      `json:"octets_in" db:"octets_in"`   // BIGINT
	OctetsOut  int64      `json:"octets_out" db:"octets_out"` // BIGINT
	Amount     float64    `json:"amount" db:"amount"`         // NUMERIC(20,10)
	StartedAt  *time.Time `json:"started_at" db:"started_at"`
	UpdatedAt  *time.Time `json:"updated_at" db:"updated_at"`
	FinishedAt *time.Time `json:"finished_at" db:"finished_at"`
	Expired    *bool      `json:"expired" db:"expired"`
}

// DBSessionDetail - таблица session_details
type DBSessionDetail struct {
	ID           int    `json:"id" db:"id"` // REFERENCES iptraffic_sessions
	TrafficClass string `json:"traffic_class" db:"traffic_class"`
	OctetsIn     int64  `json:"octets_in" db:"octets_in"`
	OctetsOut    int64  `json:"octets_out" db:"octets_out"`
}

// DBAdmin - таблица admins
type DBAdmin struct {
	ID        int       `json:"id" db:"id"`
	Email     string    `json:"email" db:"email"`
	Active    bool      `json:"active" db:"active"`
	Password  string    `json:"password" db:"password"`
	RealName  string    `json:"real_name" db:"real_name"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	Roles     string    `json:"roles" db:"roles"`
}

// DBContractInfoItem - таблица contract_info_items
type DBContractInfoItem struct {
	KindID           int    `json:"kind_id" db:"kind_id"`
	ID               int    `json:"id" db:"id"`
	SortOrder        int    `json:"sort_order" db:"sort_order"`
	FieldName        string `json:"field_name" db:"field_name"`
	FieldDescription string `json:"field_description" db:"field_description"`
}

// DBContractInfo - таблица contract_info
type DBContractInfo struct {
	ID         int    `json:"id" db:"id"`
	KindID     int    `json:"kind_id" db:"kind_id"`
	ContractID int    `json:"contract_id" db:"contract_id"`
	InfoID     int    `json:"info_id" db:"info_id"`
	InfoValue  string `json:"info_value" db:"info_value"`
}

// ================ ЗАПРОСЫ ДЛЯ СОВМЕСТИМОСТИ С ERLANG КОДОМ ================

// AccountWithRelations - результат запроса fetch_account (точно как в Erlang)
type AccountWithRelations struct {
	ID       int     `db:"id"`
	Password string  `db:"password"`
	PData    string  `db:"plan_data"` // JSON как VARCHAR
	PId      int     `db:"plan_id"`
	Auth     string  `db:"auth_algo"`
	Acct     string  `db:"acct_algo"`
	Balance  float64 `db:"balance"`
	Currency int     `db:"currency_id"`
	Credit   float64 `db:"credit"` // COALESCE(sp.credit, 0.0)
}

// ServiceParams - для получения кредита (как в Erlang коде)
type ServiceParams struct {
	AccountID int     `db:"account_id"`
	Credit    float64 `db:"credit"`
}

// ================ HELPER МЕТОДЫ ================

// ParsePlanData - парсинг JSON из VARCHAR поля plan_data
func (a *DBAccount) ParsePlanData() (map[string]interface{}, error) {
	if a.PlanData == "" {
		return make(map[string]interface{}), nil
	}

	var data map[string]interface{}
	err := json.Unmarshal([]byte(a.PlanData), &data)
	return data, err
}

// SetPlanData - сериализация в JSON для поля plan_data
func (a *DBAccount) SetPlanData(data map[string]interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	a.PlanData = string(jsonData)
	return nil
}

// ================ SQL ЗАПРОСЫ (КАК В ERLANG) ================

// SQL запросы точно как в оригинальном Erlang коде
const (
	// Запрос fetch_account из mod_iptraffic_pgsql.erl
	FetchAccountQuery = `
		SELECT a.id, a.password, a.plan_data, a.plan_id,
			p.auth_algo, p.acct_algo, c.balance, c.currency_id, COALESCE(sp.credit, 0.0)
		FROM accounts a 
		LEFT OUTER JOIN service_params sp ON a.id=sp.account_id, 
		plans p, contracts c
		WHERE a.active AND a.login=$1 AND a.plan_id=p.id AND a.contract_id=c.id`

	// Запрос fetch_radius_avpairs из mod_iptraffic_pgsql.erl
	FetchRadiusRepliesQuery = `
		SELECT a.name, v.value FROM radius_replies a, assigned_radius_replies v
		WHERE a.active AND a.id = v.radius_reply_id
		AND ((v.target_type='Account' AND v.target_id=$1) OR (v.target_type='Plan' AND v.target_id=$2))`

	// Запрос start_session из mod_iptraffic_pgsql.erl (с CID!)
	StartSessionQuery = `
		INSERT INTO iptraffic_sessions(account_id, ip, sid, cid, started_at)
		VALUES ($1, $2, $3, $4, $5)`

	// Запрос sync_session из mod_iptraffic_pgsql.erl
	SyncSessionQuery = `
		UPDATE iptraffic_sessions SET octets_in = $1, octets_out = $2,
		updated_at = $3, amount = $4
		WHERE sid = $5 AND account_id = $6`

	// Запрос stop_session из mod_iptraffic_pgsql.erl
	StopSessionQuery = `
		SELECT id FROM iptraffic_sessions
		WHERE sid = $1 AND finished_at IS NULL AND account_id = $2 LIMIT 1`

	// Обновление сессии при завершении
	FinishSessionQuery = `
		UPDATE iptraffic_sessions
		SET octets_in = $1, octets_out = $2, amount = $3,
		finished_at = $4, expired = $5
		WHERE id = $6`

	// Обновление plan_data в accounts
	UpdateAccountPlanDataQuery = `
		UPDATE accounts SET plan_data = $1 WHERE id = $2`

	// Вставка детализации сессии
	InsertSessionDetailQuery = `
		INSERT INTO session_details (id, traffic_class, octets_in, octets_out) 
		VALUES ($1, $2, $3, $4)`

	// Вызов функций транзакций (как в Erlang)
	DebitTransactionQuery  = `SELECT debit_transaction($1, $2, $3, $4)`
	CreditTransactionQuery = `SELECT credit_transaction($1, $2, $3, $4)`
)
