package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"

	"isp-billing/internal/models"
)

type PostgreSQL struct {
	db *sql.DB
}

type Config struct {
	Host               string `yaml:"host"`
	Port               int    `yaml:"port"`
	Name               string `yaml:"name"`
	User               string `yaml:"user"`
	Password           string `yaml:"password"`
	SSLMode            string `yaml:"sslmode"`
	MaxConnections     int    `yaml:"max_connections"`
	MaxIdleConnections int    `yaml:"max_idle_connections"`
}

func NewPostgreSQL(cfg Config) (*PostgreSQL, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Name, cfg.SSLMode)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(cfg.MaxConnections)
	db.SetMaxIdleConns(cfg.MaxIdleConnections)
	db.SetConnMaxLifetime(time.Hour)

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &PostgreSQL{db: db}, nil
}

func (p *PostgreSQL) Close() error {
	return p.db.Close()
}

// GetDB returns the underlying database connection
func (p *PostgreSQL) GetDB() *sql.DB {
	return p.db
}

// ================ ТОЧНЫЕ РЕАЛИЗАЦИИ ERLANG ЗАПРОСОВ ================

// FetchAccount - точная копия fetch_account из mod_iptraffic_pgsql.erl
func (p *PostgreSQL) FetchAccount(userName string) (*models.AccountWithRelations, error) {
	var account models.AccountWithRelations

	err := p.db.QueryRow(models.FetchAccountQuery, userName).Scan(
		&account.ID,
		&account.Password,
		&account.PData,
		&account.PId,
		&account.Auth,
		&account.Acct,
		&account.Balance,
		&account.Currency,
		&account.Credit,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Возвращаем nil как в Erlang коде (undefined)
		}
		return nil, fmt.Errorf("failed to fetch account: %w", err)
	}

	return &account, nil
}

// FetchRadiusReplies - точная копия fetch_radius_avpairs из mod_iptraffic_pgsql.erl
func (p *PostgreSQL) FetchRadiusReplies(userID, planID int) ([]models.RADIUSReply, error) {
	rows, err := p.db.Query(models.FetchRadiusRepliesQuery, userID, planID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch radius replies: %w", err)
	}
	defer rows.Close()

	var replies []models.RADIUSReply
	for rows.Next() {
		var reply models.RADIUSReply
		err := rows.Scan(&reply.Name, &reply.Value)
		if err != nil {
			return nil, fmt.Errorf("failed to scan radius reply: %w", err)
		}
		replies = append(replies, reply)
	}

	return replies, rows.Err()
}

// StartSession - точная копия start_session из mod_iptraffic_pgsql.erl (с CID!)
func (p *PostgreSQL) StartSession(userID int, ip, sid, cid string, startedAt time.Time) error {
	logrus.Infof("Saving session to DB: UserID=%d, IP=%s, SID=%s, MAC=%s", userID, ip, sid, cid)

	result, err := p.db.Exec(models.StartSessionQuery, userID, ip, sid, cid, startedAt)
	if err != nil {
		return fmt.Errorf("failed to start session: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("expected 1 row affected, got %d", rowsAffected)
	}

	logrus.Infof("DB insert result: success for MAC=%s", cid)
	return nil
}

// SyncSession - точная копия sync_session из mod_iptraffic_pgsql.erl
func (p *PostgreSQL) SyncSession(octetsIn, octetsOut int64, updatedAt time.Time, amount float64, sid string, userID int) error {
	result, err := p.db.Exec(models.SyncSessionQuery,
		octetsIn, octetsOut, updatedAt, amount, sid, userID)
	if err != nil {
		return fmt.Errorf("failed to sync session: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("expected 1 row affected, got %d", rowsAffected)
	}

	return nil
}

// StopSession - точная копия stop_session из mod_iptraffic_pgsql.erl
func (p *PostgreSQL) StopSession(sid string, userID int, octetsIn, octetsOut int64, amount float64, finishedAt time.Time, expired bool, planData map[string]interface{}, sessionDetails map[string]models.TrafficClass) error {
	// Начинаем транзакцию
	tx, err := p.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Получаем ID сессии
	var sessionID int
	err = tx.QueryRow(models.StopSessionQuery, sid, userID).Scan(&sessionID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil // Сессия уже завершена или не найдена
		}
		return fmt.Errorf("failed to find session: %w", err)
	}

	// Списываем деньги с баланса
	comment := fmt.Sprintf("session %d", sessionID)
	var newBalance float64
	err = tx.QueryRow(models.DebitTransactionQuery, userID, amount, comment, nil).Scan(&newBalance)
	if err != nil {
		return fmt.Errorf("failed to debit transaction: %w", err)
	}

	// Обновляем сессию
	_, err = tx.Exec(models.FinishSessionQuery,
		octetsIn, octetsOut, amount, finishedAt, expired, sessionID)
	if err != nil {
		return fmt.Errorf("failed to finish session: %w", err)
	}

	// Обновляем plan_data в accounts
	planDataJSON, err := json.Marshal(planData)
	if err != nil {
		return fmt.Errorf("failed to marshal plan data: %w", err)
	}
	_, err = tx.Exec(models.UpdateAccountPlanDataQuery, string(planDataJSON), userID)
	if err != nil {
		return fmt.Errorf("failed to update account plan data: %w", err)
	}

	// Сохраняем детали сессии по классам трафика
	for class, details := range sessionDetails {
		_, err = tx.Exec(models.InsertSessionDetailQuery,
			sessionID, class, details.OctetsIn, details.OctetsOut)
		if err != nil {
			return fmt.Errorf("failed to insert session detail for class %s: %w", class, err)
		}
	}

	return tx.Commit()
}

// ================ ДОПОЛНИТЕЛЬНЫЕ МЕТОДЫ ДЛЯ GO СИСТЕМЫ ================

// GetActiveSessions - получить активные сессии
func (p *PostgreSQL) GetActiveSessions() ([]models.DBIPTrafficSession, error) {
	query := `
		SELECT id, account_id, sid, cid, ip, octets_in, octets_out, amount,
			started_at, updated_at, finished_at, expired
		FROM iptraffic_sessions
		WHERE finished_at IS NULL
		ORDER BY started_at DESC`

	rows, err := p.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get active sessions: %w", err)
	}
	defer rows.Close()

	var sessions []models.DBIPTrafficSession
	for rows.Next() {
		var session models.DBIPTrafficSession
		err := rows.Scan(
			&session.ID, &session.AccountID, &session.SID, &session.CID,
			&session.IP, &session.OctetsIn, &session.OctetsOut, &session.Amount,
			&session.StartedAt, &session.UpdatedAt, &session.FinishedAt, &session.Expired,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		sessions = append(sessions, session)
	}

	return sessions, rows.Err()
}

// GetSessionByID - получить сессию по ID
func (p *PostgreSQL) GetSessionByID(sessionID int) (*models.DBIPTrafficSession, error) {
	query := `
		SELECT id, account_id, sid, cid, ip, octets_in, octets_out, amount,
			started_at, updated_at, finished_at, expired
		FROM iptraffic_sessions
		WHERE id = $1`

	var session models.DBIPTrafficSession
	err := p.db.QueryRow(query, sessionID).Scan(
		&session.ID, &session.AccountID, &session.SID, &session.CID,
		&session.IP, &session.OctetsIn, &session.OctetsOut, &session.Amount,
		&session.StartedAt, &session.UpdatedAt, &session.FinishedAt, &session.Expired,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get session by ID: %w", err)
	}

	return &session, nil
}

// GetSessionBySID - получить сессию по SID
func (p *PostgreSQL) GetSessionBySID(sid string) (*models.DBIPTrafficSession, error) {
	query := `
		SELECT id, account_id, sid, cid, ip, octets_in, octets_out, amount,
			started_at, updated_at, finished_at, expired
		FROM iptraffic_sessions
		WHERE sid = $1 AND finished_at IS NULL`

	var session models.DBIPTrafficSession
	err := p.db.QueryRow(query, sid).Scan(
		&session.ID, &session.AccountID, &session.SID, &session.CID,
		&session.IP, &session.OctetsIn, &session.OctetsOut, &session.Amount,
		&session.StartedAt, &session.UpdatedAt, &session.FinishedAt, &session.Expired,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get session by SID: %w", err)
	}

	return &session, nil
}

// GetSessionStats - статистика сессий
func (p *PostgreSQL) GetSessionStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Активные сессии
	var activeCount int
	err := p.db.QueryRow("SELECT COUNT(*) FROM iptraffic_sessions WHERE finished_at IS NULL").Scan(&activeCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get active sessions count: %w", err)
	}
	stats["active_sessions"] = activeCount

	// Общее количество сессий за последние 24 часа
	var totalCount int
	err = p.db.QueryRow(`
		SELECT COUNT(*) FROM iptraffic_sessions 
		WHERE started_at > NOW() - INTERVAL '24 hours'`).Scan(&totalCount)
	if err != nil {
		return nil, fmt.Errorf("failed to get total sessions count: %w", err)
	}
	stats["sessions_24h"] = totalCount

	// Общий трафик активных сессий
	var totalOctetsIn, totalOctetsOut int64
	err = p.db.QueryRow(`
		SELECT COALESCE(SUM(octets_in), 0), COALESCE(SUM(octets_out), 0) 
		FROM iptraffic_sessions 
		WHERE finished_at IS NULL`).Scan(&totalOctetsIn, &totalOctetsOut)
	if err != nil {
		return nil, fmt.Errorf("failed to get traffic stats: %w", err)
	}
	stats["total_octets_in"] = totalOctetsIn
	stats["total_octets_out"] = totalOctetsOut

	return stats, nil
}

// ParsePlanDataFromJSON - парсинг plan_data из JSON строки (как в Erlang)
func ParsePlanDataFromJSON(jsonStr string) (map[string]interface{}, error) {
	if jsonStr == "" {
		return make(map[string]interface{}), nil
	}

	var data map[string]interface{}
	err := json.Unmarshal([]byte(jsonStr), &data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse plan data JSON: %w", err)
	}

	return data, nil
}

// SplitAlgoName - разбор имени алгоритма как в Erlang (algo_builtin:prepaid_auth -> module:function)
func SplitAlgoName(algoName string) (string, string) {
	parts := strings.SplitN(algoName, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "algo_builtin", algoName // default module
}
