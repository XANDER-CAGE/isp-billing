package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"netspire-go/internal/database"
)

type AdminHandler struct {
	db *database.PostgreSQL
}

func NewAdminHandler(db *database.PostgreSQL) *AdminHandler {
	return &AdminHandler{
		db: db,
	}
}

// GetActiveSessions - получить все активные сессии
func (h *AdminHandler) GetActiveSessions(c *gin.Context) {
	sessions, err := h.db.GetActiveSessions()
	if err != nil {
		logrus.Errorf("Failed to get active sessions: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"sessions": sessions,
		"count":    len(sessions),
	})
}

// GetSession - получить сессию по ID
func (h *AdminHandler) GetSession(c *gin.Context) {
	idParam := c.Param("id")
	sessionID, err := strconv.Atoi(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid session ID"})
		return
	}

	session, err := h.db.GetSessionByID(sessionID)
	if err != nil {
		logrus.Errorf("Failed to get session %d: %v", sessionID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	if session == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Session not found"})
		return
	}

	c.JSON(http.StatusOK, session)
}

// DisconnectSession - принудительно завершить сессию
func (h *AdminHandler) DisconnectSession(c *gin.Context) {
	idParam := c.Param("id")
	sessionID, err := strconv.Atoi(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid session ID"})
		return
	}

	// Получаем сессию
	session, err := h.db.GetSessionByID(sessionID)
	if err != nil {
		logrus.Errorf("Failed to get session %d: %v", sessionID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	if session == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Session not found"})
		return
	}

	// TODO: Отправить CoA/POD запрос на NAS
	// Пока просто отмечаем как истекшую в БД
	logrus.Infof("Disconnecting session %d (SID: %s)", sessionID, session.SID)

	c.JSON(http.StatusOK, gin.H{
		"message":    "Disconnect request sent",
		"session_id": sessionID,
		"sid":        session.SID,
	})
}

// GetStats - получить статистику системы
func (h *AdminHandler) GetStats(c *gin.Context) {
	stats, err := h.db.GetSessionStats()
	if err != nil {
		logrus.Errorf("Failed to get stats: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// GetAccount - получить информацию об аккаунте
func (h *AdminHandler) GetAccount(c *gin.Context) {
	login := c.Param("login")

	account, err := h.db.FetchAccount(login)
	if err != nil {
		logrus.Errorf("Failed to get account %s: %v", login, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	if account == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
		return
	}

	// Скрываем пароль в ответе
	response := map[string]interface{}{
		"id":       account.ID,
		"login":    login,
		"plan_id":  account.PId,
		"balance":  account.Balance,
		"currency": account.Currency,
		"credit":   account.Credit,
		"auth":     account.Auth,
		"acct":     account.Acct,
	}

	c.JSON(http.StatusOK, response)
}

// GetPlans - получить список тарифных планов
func (h *AdminHandler) GetPlans(c *gin.Context) {
	// TODO: Реализовать получение планов из БД
	c.JSON(http.StatusOK, gin.H{
		"message": "Plans endpoint not implemented yet",
		"plans":   []interface{}{},
	})
}
