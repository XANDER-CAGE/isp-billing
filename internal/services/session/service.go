package session

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"netspire-go/internal/database"
	"netspire-go/internal/models"
	"netspire-go/internal/services/billing"
	"netspire-go/internal/services/disconnect"
	"netspire-go/internal/services/ippool"

	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	DefaultSessionTimeout = 60 // Default session timeout in seconds
	DefaultSyncInterval   = 30 // Sync to DB every 30 seconds
	RedisSessionPrefix    = "session:"
	RedisSessionsByIP     = "sessions_by_ip:"
	RedisSessionsByUser   = "sessions_by_user:"
)

// Service handles session management
// Full equivalent to iptraffic_session.erl and iptraffic_sup.erl functionality
type Service struct {
	redis      *redis.Client
	db         *database.PostgreSQL
	billing    *billing.Service
	ippool     *ippool.Service
	disconnect *disconnect.Service
	logger     *zap.Logger
	config     Config

	// Internal state
	sessions    map[string]*models.IPTrafficSession // UUID -> Session
	sessionsMux sync.RWMutex

	// Worker management
	workers    map[string]*SessionWorker // UUID -> Worker
	workersMux sync.RWMutex

	// Background tasks
	syncTicker    *time.Ticker
	cleanupTicker *time.Ticker
	stopChan      chan struct{}
	wg            sync.WaitGroup
}

// Config holds session service configuration
// Equivalent to mod_iptraffic options in Erlang config
type Config struct {
	SessionTimeout       int  `yaml:"session_timeout"`        // Session timeout in seconds
	SyncInterval         int  `yaml:"sync_interval"`          // DB sync interval in seconds
	DelayStop            int  `yaml:"delay_stop"`             // Delay before stopping session
	DisconnectOnShutdown bool `yaml:"disconnect_on_shutdown"` // Disconnect clients on shutdown
	MaxSessions          int  `yaml:"max_sessions"`           // Maximum concurrent sessions
	CleanupInterval      int  `yaml:"cleanup_interval"`       // Cleanup interval in seconds
}

// SessionWorker represents a worker for individual session
// Equivalent to individual session process in Erlang
type SessionWorker struct {
	session  *models.IPTrafficSession
	service  *Service
	stopChan chan struct{}
	timeout  *time.Timer
}

// New creates a new session service
func New(redisClient *redis.Client, db *database.PostgreSQL, billingService *billing.Service,
	ippoolService *ippool.Service, disconnectService *disconnect.Service, logger *zap.Logger, config Config) *Service {

	// Set defaults like in Erlang
	if config.SessionTimeout == 0 {
		config.SessionTimeout = DefaultSessionTimeout
	}
	if config.SyncInterval == 0 {
		config.SyncInterval = DefaultSyncInterval
	}
	if config.DelayStop == 0 {
		config.DelayStop = 5
	}
	if config.CleanupInterval == 0 {
		config.CleanupInterval = 30
	}

	return &Service{
		redis:      redisClient,
		db:         db,
		billing:    billingService,
		ippool:     ippoolService,
		disconnect: disconnectService,
		logger:     logger,
		config:     config,
		sessions:   make(map[string]*models.IPTrafficSession),
		workers:    make(map[string]*SessionWorker),
		stopChan:   make(chan struct{}),
	}
}

// Start initializes session service
// Equivalent to start/1 in iptraffic_sup.erl
func (s *Service) Start() error {
	s.logger.Info("Starting session service",
		zap.Int("session_timeout", s.config.SessionTimeout),
		zap.Int("sync_interval", s.config.SyncInterval))

	// Load existing sessions from Redis
	if err := s.loadExistingSessions(); err != nil {
		s.logger.Error("Failed to load existing sessions", zap.Error(err))
		// Continue anyway, don't fail startup
	}

	// Start background tasks
	s.startBackgroundTasks()

	return nil
}

// Stop gracefully shuts down session service
// Equivalent to stop/0 in iptraffic_sup.erl
func (s *Service) Stop() error {
	s.logger.Info("Stopping session service")

	// Signal all workers to stop
	close(s.stopChan)

	// Disconnect all sessions if configured
	if s.config.DisconnectOnShutdown {
		s.disconnectAllSessions()
	}

	// Stop background tasks
	if s.syncTicker != nil {
		s.syncTicker.Stop()
	}
	if s.cleanupTicker != nil {
		s.cleanupTicker.Stop()
	}

	// Wait for workers to finish
	s.wg.Wait()

	// Final sync to database
	s.syncAllSessions()

	s.logger.Info("Session service stopped")
	return nil
}

// InitSession creates a new session for user
// Equivalent to init_session/1 in iptraffic_sup.erl
func (s *Service) InitSession(username string) (*models.IPTrafficSession, error) {
	s.sessionsMux.Lock()
	defer s.sessionsMux.Unlock()

	// Check if user already has a session
	if existingSession := s.findSessionByUsername(username); existingSession != nil {
		if existingSession.IsActive() {
			return nil, fmt.Errorf("user %s already has an active session", username)
		}
		// Clean up old session
		s.cleanupSession(existingSession.UUID)
	}

	// Create new session
	sessionUUID := uuid.New().String()
	session := models.NewIPTrafficSession(sessionUUID, username)

	// Store in memory and Redis
	s.sessions[sessionUUID] = session
	if err := s.saveSessionToRedis(session); err != nil {
		delete(s.sessions, sessionUUID)
		return nil, fmt.Errorf("failed to save session to Redis: %w", err)
	}

	// Index by username
	s.indexSessionByUsername(username, sessionUUID)

	s.logger.Info("Session initialized",
		zap.String("uuid", sessionUUID),
		zap.String("username", username))

	return session, nil
}

// PrepareSession prepares session with context data
// Equivalent to prepare/5 in iptraffic_session.erl
func (s *Service) PrepareSession(sessionUUID string, ctx *models.SessionContext) error {
	s.sessionsMux.Lock()
	defer s.sessionsMux.Unlock()

	session, exists := s.sessions[sessionUUID]
	if !exists {
		return fmt.Errorf("session not found: %s", sessionUUID)
	}

	// Prepare session with context
	if err := session.Prepare(ctx); err != nil {
		return fmt.Errorf("failed to prepare session: %w", err)
	}

	// Save updated session
	if err := s.saveSessionToRedis(session); err != nil {
		return fmt.Errorf("failed to save prepared session: %w", err)
	}

	s.logger.Info("Session prepared",
		zap.String("uuid", sessionUUID),
		zap.String("username", session.Username),
		zap.Int("plan_id", session.PlanID))

	return nil
}

// StartSession activates session with accounting start
// Equivalent to start/4 in iptraffic_session.erl
func (s *Service) StartSession(username, sid, cid string, ip net.IP) error {
	session := s.findSessionByUsername(username)
	if session == nil {
		return fmt.Errorf("no prepared session found for user %s", username)
	}

	s.sessionsMux.Lock()
	defer s.sessionsMux.Unlock()

	// Activate session
	session.Activate(sid, cid, ip)

	// Create database session record
	dbSessionID, err := s.createDBSession(session)
	if err != nil {
		return fmt.Errorf("failed to create database session: %w", err)
	}
	session.DBSessionID = dbSessionID

	// Index by IP and SID
	s.indexSessionByIP(ip.String(), session.UUID)
	s.indexSessionBySID(sid, session.UUID)

	// Start session worker
	if err := s.startSessionWorker(session); err != nil {
		return fmt.Errorf("failed to start session worker: %w", err)
	}

	// Save updated session
	if err := s.saveSessionToRedis(session); err != nil {
		return fmt.Errorf("failed to save active session: %w", err)
	}

	s.logger.Info("Session started",
		zap.String("uuid", session.UUID),
		zap.String("username", username),
		zap.String("sid", sid),
		zap.String("ip", ip.String()),
		zap.String("cid", cid),
		zap.Int64("db_session_id", dbSessionID))

	return nil
}

// InterimUpdate handles interim accounting updates
// Equivalent to interim/1 in iptraffic_session.erl
func (s *Service) InterimUpdate(sid string) error {
	session := s.findSessionBySID(sid)
	if session == nil {
		return fmt.Errorf("session not found for SID: %s", sid)
	}

	s.sessionsMux.Lock()
	defer s.sessionsMux.Unlock()

	// Renew session timeout
	session.RenewTimeout(s.config.SessionTimeout)

	// Renew IP lease if IP pool is configured
	if s.ippool != nil && session.IP != nil {
		if err := s.ippool.Renew(session.IP); err != nil {
			s.logger.Warn("Failed to renew IP lease",
				zap.String("sid", sid),
				zap.String("ip", session.IP.String()),
				zap.Error(err))
		}
	}

	// Save updated session
	if err := s.saveSessionToRedis(session); err != nil {
		return fmt.Errorf("failed to save session after interim: %w", err)
	}

	s.logger.Debug("Session interim update",
		zap.String("sid", sid),
		zap.String("username", session.Username))

	return nil
}

// StopSession handles accounting stop
// Equivalent to stop/1 in iptraffic_session.erl
func (s *Service) StopSession(sid string) error {
	session := s.findSessionBySID(sid)
	if session == nil {
		return fmt.Errorf("session not found for SID: %s", sid)
	}

	s.sessionsMux.Lock()
	defer s.sessionsMux.Unlock()

	// Mark session as stopping
	session.Status = models.StatusStopping

	// Save final session state to database
	if err := s.syncSessionToDB(session); err != nil {
		s.logger.Error("Failed to sync session before stop", zap.Error(err))
	}

	// Stop session after delay (like in Erlang delay_stop)
	go s.delayedStopSession(session, s.config.DelayStop)

	s.logger.Info("Session stop initiated",
		zap.String("sid", sid),
		zap.String("username", session.Username),
		zap.Int("delay_stop", s.config.DelayStop))

	return nil
}

// ExpireSession marks session as expired
// Equivalent to expire/1 in iptraffic_session.erl
func (s *Service) ExpireSession(sessionUUID string) error {
	s.sessionsMux.Lock()
	defer s.sessionsMux.Unlock()

	session, exists := s.sessions[sessionUUID]
	if !exists {
		return fmt.Errorf("session not found: %s", sessionUUID)
	}

	session.Expire()

	// Sync to database
	if err := s.syncSessionToDB(session); err != nil {
		s.logger.Error("Failed to sync expired session", zap.Error(err))
	}

	// Send disconnect if configured
	if s.disconnect != nil && session.IP != nil {
		go func() {
			err := s.disconnect.DisconnectSession(session.Username, session.SID, session.IP, session.NASSpec)
			if err != nil {
				s.logger.Error("Failed to disconnect expired session", zap.Error(err))
			}
		}()
	}

	s.logger.Info("Session expired",
		zap.String("uuid", sessionUUID),
		zap.String("username", session.Username))

	// Cleanup after delay
	go s.delayedCleanupSession(sessionUUID, 5)

	return nil
}

// HandleNetFlow processes NetFlow data for session
// Equivalent to handle_cast({netflow, Dir, {H, Rec}}) in iptraffic_session.erl
func (s *Service) HandleNetFlow(direction string, srcIP, dstIP net.IP, octets, packets uint64) error {
	// Determine target IP and find session
	var targetIP net.IP
	if direction == "in" {
		targetIP = dstIP
	} else {
		targetIP = srcIP
	}

	session := s.findSessionByIP(targetIP.String())
	if session == nil {
		// No active session for this IP
		return nil
	}

	s.sessionsMux.Lock()
	defer s.sessionsMux.Unlock()

	// Classify traffic
	class := s.classifyTraffic(targetIP.String())

	// Call billing algorithm for this traffic
	amount, newPlanData, err := s.performAccounting(session, direction, targetIP.String(), octets, class)
	if err != nil {
		s.logger.Error("Billing accounting failed",
			zap.String("session", session.UUID),
			zap.Error(err))
		return err
	}

	// Update session with traffic and billing data
	session.UpdateTrafficByClass(class, direction, octets, packets, amount)

	// Update plan data if changed
	if newPlanData != nil {
		session.UpdatePlanData(newPlanData)
	}

	// Save updated session
	if err := s.saveSessionToRedis(session); err != nil {
		s.logger.Error("Failed to save session after NetFlow", zap.Error(err))
	}

	s.logger.Debug("NetFlow processed",
		zap.String("session", session.UUID),
		zap.String("direction", direction),
		zap.Uint64("octets", octets),
		zap.String("class", class),
		zap.Float64("amount", amount))

	return nil
}

// FindSessionByIP returns session by IP address
func (s *Service) FindSessionByIP(ip string) *models.IPTrafficSession {
	return s.findSessionByIP(ip)
}

// FindSessionByUsername returns session by username
func (s *Service) FindSessionByUsername(username string) *models.IPTrafficSession {
	return s.findSessionByUsername(username)
}

// FindSessionBySID returns session by session ID
func (s *Service) FindSessionBySID(sid string) *models.IPTrafficSession {
	return s.findSessionBySID(sid)
}

// GetAllSessions returns all active sessions
func (s *Service) GetAllSessions() []*models.IPTrafficSession {
	s.sessionsMux.RLock()
	defer s.sessionsMux.RUnlock()

	sessions := make([]*models.IPTrafficSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	return sessions
}

// GetSessionStats returns session statistics
func (s *Service) GetSessionStats() map[string]interface{} {
	s.sessionsMux.RLock()
	defer s.sessionsMux.RUnlock()

	stats := make(map[string]interface{})

	totalSessions := len(s.sessions)
	activeSessions := 0
	expiredSessions := 0
	stoppedSessions := 0

	for _, session := range s.sessions {
		switch session.Status {
		case models.StatusActive:
			activeSessions++
		case models.StatusExpired:
			expiredSessions++
		case models.StatusStopped:
			stoppedSessions++
		}
	}

	stats["total_sessions"] = totalSessions
	stats["active_sessions"] = activeSessions
	stats["expired_sessions"] = expiredSessions
	stats["stopped_sessions"] = stoppedSessions
	stats["max_sessions"] = s.config.MaxSessions

	return stats
}

// Internal helper methods

func (s *Service) loadExistingSessions() error {
	ctx := context.Background()
	keys, err := s.redis.Keys(ctx, RedisSessionPrefix+"*").Result()
	if err != nil {
		return err
	}

	for _, key := range keys {
		sessionData, err := s.redis.HGetAll(ctx, key).Result()
		if err != nil {
			s.logger.Warn("Failed to load session", zap.String("key", key), zap.Error(err))
			continue
		}

		session := &models.IPTrafficSession{}
		if err := session.FromRedisHash(sessionData); err != nil {
			s.logger.Warn("Failed to parse session", zap.String("key", key), zap.Error(err))
			continue
		}

		s.sessions[session.UUID] = session

		// Rebuild indexes
		if session.Username != "" {
			s.indexSessionByUsername(session.Username, session.UUID)
		}
		if session.IP != nil {
			s.indexSessionByIP(session.IP.String(), session.UUID)
		}
		if session.SID != "" {
			s.indexSessionBySID(session.SID, session.UUID)
		}

		// Restart worker if session is active
		if session.IsActive() && !session.IsExpired() {
			s.startSessionWorker(session)
		}
	}

	s.logger.Info("Loaded existing sessions", zap.Int("count", len(s.sessions)))
	return nil
}

func (s *Service) startBackgroundTasks() {
	// Sync task
	s.syncTicker = time.NewTicker(time.Duration(s.config.SyncInterval) * time.Second)
	s.wg.Add(1)
	go s.syncTask()

	// Cleanup task
	s.cleanupTicker = time.NewTicker(time.Duration(s.config.CleanupInterval) * time.Second)
	s.wg.Add(1)
	go s.cleanupTask()
}

func (s *Service) syncTask() {
	defer s.wg.Done()

	for {
		select {
		case <-s.syncTicker.C:
			s.syncAllSessions()
		case <-s.stopChan:
			return
		}
	}
}

func (s *Service) cleanupTask() {
	defer s.wg.Done()

	for {
		select {
		case <-s.cleanupTicker.C:
			s.cleanupExpiredSessions()
		case <-s.stopChan:
			return
		}
	}
}

func (s *Service) syncAllSessions() {
	s.sessionsMux.RLock()
	sessions := make([]*models.IPTrafficSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		if session.NeedsSync() {
			sessions = append(sessions, session)
		}
	}
	s.sessionsMux.RUnlock()

	for _, session := range sessions {
		if err := s.syncSessionToDB(session); err != nil {
			s.logger.Error("Failed to sync session",
				zap.String("session", session.UUID),
				zap.Error(err))
		}
	}

	if len(sessions) > 0 {
		s.logger.Debug("Synced sessions to database", zap.Int("count", len(sessions)))
	}
}

func (s *Service) cleanupExpiredSessions() {
	s.sessionsMux.Lock()
	defer s.sessionsMux.Unlock()

	now := time.Now().Unix()
	expiredSessions := make([]string, 0)

	for uuid, session := range s.sessions {
		if session.ExpiresAt <= now && session.Status == models.StatusActive {
			s.ExpireSession(uuid)
			expiredSessions = append(expiredSessions, uuid)
		}
	}

	if len(expiredSessions) > 0 {
		s.logger.Info("Expired sessions cleaned up", zap.Int("count", len(expiredSessions)))
	}
}

func (s *Service) findSessionByIP(ip string) *models.IPTrafficSession {
	ctx := context.Background()
	sessionUUID, err := s.redis.Get(ctx, RedisSessionsByIP+ip).Result()
	if err != nil {
		return nil
	}

	s.sessionsMux.RLock()
	session, exists := s.sessions[sessionUUID]
	s.sessionsMux.RUnlock()

	if !exists || !session.IsActive() {
		return nil
	}

	return session
}

func (s *Service) findSessionByUsername(username string) *models.IPTrafficSession {
	ctx := context.Background()
	sessionUUID, err := s.redis.Get(ctx, RedisSessionsByUser+username).Result()
	if err != nil {
		return nil
	}

	s.sessionsMux.RLock()
	session, exists := s.sessions[sessionUUID]
	s.sessionsMux.RUnlock()

	if !exists {
		return nil
	}

	return session
}

func (s *Service) findSessionBySID(sid string) *models.IPTrafficSession {
	ctx := context.Background()
	sessionUUID, err := s.redis.Get(ctx, "session_by_sid:"+sid).Result()
	if err != nil {
		return nil
	}

	s.sessionsMux.RLock()
	session, exists := s.sessions[sessionUUID]
	s.sessionsMux.RUnlock()

	if !exists {
		return nil
	}

	return session
}

func (s *Service) indexSessionByIP(ip, sessionUUID string) {
	ctx := context.Background()
	s.redis.Set(ctx, RedisSessionsByIP+ip, sessionUUID, time.Duration(s.config.SessionTimeout*2)*time.Second)
}

func (s *Service) indexSessionByUsername(username, sessionUUID string) {
	ctx := context.Background()
	s.redis.Set(ctx, RedisSessionsByUser+username, sessionUUID, time.Duration(s.config.SessionTimeout*2)*time.Second)
}

func (s *Service) indexSessionBySID(sid, sessionUUID string) {
	ctx := context.Background()
	s.redis.Set(ctx, "session_by_sid:"+sid, sessionUUID, time.Duration(s.config.SessionTimeout*2)*time.Second)
}

func (s *Service) saveSessionToRedis(session *models.IPTrafficSession) error {
	ctx := context.Background()
	key := RedisSessionPrefix + session.UUID

	hash := session.ToRedisHash()
	return s.redis.HMSet(ctx, key, hash).Err()
}

func (s *Service) startSessionWorker(session *models.IPTrafficSession) error {
	s.workersMux.Lock()
	defer s.workersMux.Unlock()

	worker := &SessionWorker{
		session:  session,
		service:  s,
		stopChan: make(chan struct{}),
		timeout:  time.NewTimer(time.Duration(s.config.SessionTimeout) * time.Second),
	}

	s.workers[session.UUID] = worker

	s.wg.Add(1)
	go worker.run()

	return nil
}

func (s *Service) delayedStopSession(session *models.IPTrafficSession, delaySec int) {
	time.Sleep(time.Duration(delaySec) * time.Second)

	// Final stop
	session.Stop()

	// Release IP if applicable
	if s.ippool != nil && session.IP != nil {
		if err := s.ippool.Release(session.IP); err != nil {
			s.logger.Error("Failed to release IP on session stop",
				zap.String("ip", session.IP.String()),
				zap.Error(err))
		}
	}

	// Final sync to DB
	if err := s.finishDBSession(session); err != nil {
		s.logger.Error("Failed to finish DB session", zap.Error(err))
	}

	// Cleanup after additional delay
	go s.delayedCleanupSession(session.UUID, 10)

	s.logger.Info("Session stopped",
		zap.String("uuid", session.UUID),
		zap.String("username", session.Username))
}

func (s *Service) delayedCleanupSession(sessionUUID string, delaySec int) {
	time.Sleep(time.Duration(delaySec) * time.Second)
	s.cleanupSession(sessionUUID)
}

func (s *Service) cleanupSession(sessionUUID string) {
	s.sessionsMux.Lock()
	defer s.sessionsMux.Unlock()

	session, exists := s.sessions[sessionUUID]
	if !exists {
		return
	}

	// Stop worker
	s.workersMux.Lock()
	if worker, exists := s.workers[sessionUUID]; exists {
		close(worker.stopChan)
		delete(s.workers, sessionUUID)
	}
	s.workersMux.Unlock()

	// Remove from memory
	delete(s.sessions, sessionUUID)

	// Remove from Redis
	ctx := context.Background()
	s.redis.Del(ctx, RedisSessionPrefix+sessionUUID)

	// Remove indexes
	if session.IP != nil {
		s.redis.Del(ctx, RedisSessionsByIP+session.IP.String())
	}
	if session.Username != "" {
		s.redis.Del(ctx, RedisSessionsByUser+session.Username)
	}
	if session.SID != "" {
		s.redis.Del(ctx, "session_by_sid:"+session.SID)
	}

	s.logger.Debug("Session cleaned up", zap.String("uuid", sessionUUID))
}

func (s *Service) disconnectAllSessions() {
	s.sessionsMux.RLock()
	sessions := make([]*models.IPTrafficSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		if session.IsActive() {
			sessions = append(sessions, session)
		}
	}
	s.sessionsMux.RUnlock()

	for _, session := range sessions {
		if s.disconnect != nil && session.IP != nil {
			err := s.disconnect.DisconnectSession(session.Username, session.SID, session.IP, session.NASSpec)
			if err != nil {
				s.logger.Error("Failed to disconnect session on shutdown",
					zap.String("username", session.Username),
					zap.Error(err))
			}
		}
	}

	s.logger.Info("Disconnected all active sessions", zap.Int("count", len(sessions)))
}

// Database operations (equivalent to mod_iptraffic_pgsql.erl functions)

func (s *Service) createDBSession(session *models.IPTrafficSession) (int64, error) {
	accountID, _ := session.GetContextValue("account_id")

	query := `INSERT INTO iptraffic_sessions(account_id, ip, sid, cid, started_at)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`

	var sessionID int64
	err := s.db.GetDB().QueryRow(query, accountID, session.IP.String(), session.SID, session.CID, time.Now()).Scan(&sessionID)
	if err != nil {
		return 0, err
	}

	return sessionID, nil
}

func (s *Service) syncSessionToDB(session *models.IPTrafficSession) error {
	if session.DBSessionID == 0 {
		return nil // Session not in DB yet
	}

	query := `UPDATE iptraffic_sessions SET octets_in = $1, octets_out = $2,
		updated_at = $3, amount = $4 WHERE id = $5`

	_, err := s.db.GetDB().Exec(query, session.InOctets, session.OutOctets, time.Now(), session.Amount, session.DBSessionID)
	if err != nil {
		return err
	}

	// Update plan data if changed
	if changed, exists := session.GetContextValue("plan_data_changed"); exists && changed.(bool) {
		accountID, _ := session.GetContextValue("account_id")
		planDataJSON, _ := json.Marshal(session.PlanData)

		_, err = s.db.GetDB().Exec(`UPDATE accounts SET plan_data = $1 WHERE id = $2`, string(planDataJSON), accountID)
		if err != nil {
			return err
		}
	}

	// Save traffic details
	if err := s.saveTrafficDetails(session); err != nil {
		return err
	}

	session.MarkSynced()
	return nil
}

func (s *Service) finishDBSession(session *models.IPTrafficSession) error {
	if session.DBSessionID == 0 {
		return nil
	}

	expired := session.Status == models.StatusExpired
	query := `UPDATE iptraffic_sessions SET octets_in = $1, octets_out = $2, amount = $3,
		finished_at = $4, expired = $5 WHERE id = $6`

	_, err := s.db.GetDB().Exec(query, session.InOctets, session.OutOctets, session.Amount,
		time.Now(), expired, session.DBSessionID)

	return err
}

func (s *Service) saveTrafficDetails(session *models.IPTrafficSession) error {
	if len(session.TrafficDetails) == 0 {
		return nil
	}

	// Delete existing details
	_, err := s.db.GetDB().Exec(`DELETE FROM session_details WHERE id = $1`, session.DBSessionID)
	if err != nil {
		return err
	}

	// Insert new details
	for _, detail := range session.TrafficDetails {
		_, err = s.db.GetDB().Exec(`INSERT INTO session_details (id, traffic_class, octets_in, octets_out) 
			VALUES ($1, $2, $3, $4)`, session.DBSessionID, detail.Class, detail.InOctets, detail.OutOctets)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) performAccounting(session *models.IPTrafficSession, direction, targetIP string, octets uint64, class string) (float64, map[string]interface{}, error) {
	// TODO: Implement proper billing integration
	// For now, simple calculation based on octets
	costPerMB := 0.01 // Default cost

	if cost, exists := session.PlanData["cost_per_mb"]; exists {
		if costFloat, ok := cost.(float64); ok {
			costPerMB = costFloat
		}
	}

	amount := float64(octets) / 1024 / 1024 * costPerMB

	// Return unchanged plan data for now
	return amount, session.PlanData, nil
}

func (s *Service) classifyTraffic(targetIP string) string {
	// Simple classification - should use traffic classification service
	ip := net.ParseIP(targetIP)
	if ip == nil {
		return "default"
	}

	// Check if it's local network
	if ip.IsPrivate() {
		return "local"
	}

	return "internet"
}

// SessionWorker methods

func (w *SessionWorker) run() {
	defer w.service.wg.Done()

	for {
		select {
		case <-w.timeout.C:
			// Session timeout
			w.service.ExpireSession(w.session.UUID)
			return
		case <-w.stopChan:
			// Explicit stop
			return
		}
	}
}
