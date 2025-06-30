package handlers

import (
	"encoding/binary"
	"fmt"
	"net"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"netspire-go/internal/database"
	"netspire-go/internal/models"
	"netspire-go/internal/services/billing"
)

// NetFlowHandler обрабатывает NetFlow пакеты
type NetFlowHandler struct {
	db      *database.PostgreSQL
	billing *billing.Service
}

func NewNetFlowHandler(db *database.PostgreSQL, billingService *billing.Service) *NetFlowHandler {
	return &NetFlowHandler{
		db:      db,
		billing: billingService,
	}
}

// NetFlow v5 структуры (как в netflow_v5.hrl)
type NetFlowV5Header struct {
	Version      uint16
	Count        uint16
	SysUptime    uint32
	UnixSecs     uint32
	UnixNanos    uint32
	FlowSequence uint32
	EngineType   uint8
	EngineID     uint8
	SamplingMode uint8
	SamplingRate uint8
}

type NetFlowV5Record struct {
	SrcAddr   uint32
	DstAddr   uint32
	NextHop   uint32
	Input     uint16
	Output    uint16
	Packets   uint32
	Octets    uint32
	FirstTime uint32
	LastTime  uint32
	SrcPort   uint16
	DstPort   uint16
	Pad1      uint8
	TCPFlags  uint8
	Protocol  uint8
	TOS       uint8
	SrcAS     uint16
	DstAS     uint16
	SrcMask   uint8
	DstMask   uint8
	Pad2      uint16
}

// NetFlow v9 структуры (как в netflow_v9.hrl)
type NetFlowV9Header struct {
	Version    uint16
	Count      uint16
	SysUptime  uint32
	UnixSecs   uint32
	PackageSeq uint32
	SourceID   uint32
}

// ProcessNetFlowV5 - обработка NetFlow v5 пакетов
func (h *NetFlowHandler) ProcessNetFlowV5(c *gin.Context) {
	// Получаем данные пакета
	data, err := c.GetRawData()
	if err != nil {
		logrus.Errorf("Failed to read NetFlow v5 data: %v", err)
		c.JSON(400, gin.H{"error": "Invalid data"})
		return
	}

	// Парсим заголовок
	if len(data) < 24 {
		logrus.Errorf("NetFlow v5 packet too small: %d bytes", len(data))
		c.JSON(400, gin.H{"error": "Packet too small"})
		return
	}

	header := NetFlowV5Header{
		Version:      binary.BigEndian.Uint16(data[0:2]),
		Count:        binary.BigEndian.Uint16(data[2:4]),
		SysUptime:    binary.BigEndian.Uint32(data[4:8]),
		UnixSecs:     binary.BigEndian.Uint32(data[8:12]),
		UnixNanos:    binary.BigEndian.Uint32(data[12:16]),
		FlowSequence: binary.BigEndian.Uint32(data[16:20]),
		EngineType:   data[20],
		EngineID:     data[21],
		SamplingMode: data[22] >> 6,
		SamplingRate: data[22] & 0x3F,
	}

	if header.Version != 5 {
		logrus.Errorf("Invalid NetFlow version: %d, expected 5", header.Version)
		c.JSON(400, gin.H{"error": "Invalid version"})
		return
	}

	logrus.Debugf("NetFlow v5: Count=%d, Sequence=%d", header.Count, header.FlowSequence)

	// Парсим записи
	recordSize := 48
	expectedSize := 24 + int(header.Count)*recordSize
	if len(data) < expectedSize {
		logrus.Errorf("NetFlow v5 packet incomplete: got %d, expected %d", len(data), expectedSize)
		c.JSON(400, gin.H{"error": "Incomplete packet"})
		return
	}

	// Обрабатываем каждую запись
	processed := 0
	for i := 0; i < int(header.Count); i++ {
		offset := 24 + i*recordSize
		recordData := data[offset : offset+recordSize]

		record := h.parseNetFlowV5Record(recordData)
		if record != nil {
			// Обрабатываем запись (как handle_packet в iptraffic_session.erl)
			h.processFlowRecord(record, &header)
			processed++
		}
	}

	logrus.Infof("NetFlow v5: Processed %d/%d records", processed, header.Count)
	c.JSON(200, gin.H{
		"status":    "ok",
		"processed": processed,
		"total":     header.Count,
	})
}

// ProcessNetFlowV9 - обработка NetFlow v9 пакетов (упрощенная версия)
func (h *NetFlowHandler) ProcessNetFlowV9(c *gin.Context) {
	// Получаем данные пакета
	data, err := c.GetRawData()
	if err != nil {
		logrus.Errorf("Failed to read NetFlow v9 data: %v", err)
		c.JSON(400, gin.H{"error": "Invalid data"})
		return
	}

	// Парсим заголовок
	if len(data) < 20 {
		logrus.Errorf("NetFlow v9 packet too small: %d bytes", len(data))
		c.JSON(400, gin.H{"error": "Packet too small"})
		return
	}

	header := NetFlowV9Header{
		Version:    binary.BigEndian.Uint16(data[0:2]),
		Count:      binary.BigEndian.Uint16(data[2:4]),
		SysUptime:  binary.BigEndian.Uint32(data[4:8]),
		UnixSecs:   binary.BigEndian.Uint32(data[8:12]),
		PackageSeq: binary.BigEndian.Uint32(data[12:16]),
		SourceID:   binary.BigEndian.Uint32(data[16:20]),
	}

	if header.Version != 9 {
		logrus.Errorf("Invalid NetFlow version: %d, expected 9", header.Version)
		c.JSON(400, gin.H{"error": "Invalid version"})
		return
	}

	logrus.Infof("NetFlow v9: Count=%d, Sequence=%d, SourceID=%d",
		header.Count, header.PackageSeq, header.SourceID)

	// NetFlow v9 более сложен - пока возвращаем успех
	c.JSON(200, gin.H{
		"status":  "ok",
		"version": 9,
		"message": "NetFlow v9 basic processing",
	})
}

// parseNetFlowV5Record - парсинг записи NetFlow v5
func (h *NetFlowHandler) parseNetFlowV5Record(data []byte) *NetFlowV5Record {
	if len(data) < 48 {
		return nil
	}

	return &NetFlowV5Record{
		SrcAddr:   binary.BigEndian.Uint32(data[0:4]),
		DstAddr:   binary.BigEndian.Uint32(data[4:8]),
		NextHop:   binary.BigEndian.Uint32(data[8:12]),
		Input:     binary.BigEndian.Uint16(data[12:14]),
		Output:    binary.BigEndian.Uint16(data[14:16]),
		Packets:   binary.BigEndian.Uint32(data[16:20]),
		Octets:    binary.BigEndian.Uint32(data[20:24]),
		FirstTime: binary.BigEndian.Uint32(data[24:28]),
		LastTime:  binary.BigEndian.Uint32(data[28:32]),
		SrcPort:   binary.BigEndian.Uint16(data[32:34]),
		DstPort:   binary.BigEndian.Uint16(data[34:36]),
		Pad1:      data[36],
		TCPFlags:  data[37],
		Protocol:  data[38],
		TOS:       data[39],
		SrcAS:     binary.BigEndian.Uint16(data[40:42]),
		DstAS:     binary.BigEndian.Uint16(data[42:44]),
		SrcMask:   data[44],
		DstMask:   data[45],
		Pad2:      binary.BigEndian.Uint16(data[46:48]),
	}
}

// processFlowRecord - обработка записи трафика (как в iptraffic_session.erl)
func (h *NetFlowHandler) processFlowRecord(record *NetFlowV5Record, header *NetFlowV5Header) {
	srcIP := intToIP(record.SrcAddr)
	dstIP := intToIP(record.DstAddr)

	logrus.Debugf("Flow: %s -> %s, %d octets, protocol %d",
		srcIP, dstIP, record.Octets, record.Protocol)

	// Определяем направление трафика и целевой IP
	// В реальной системе здесь нужна логика определения "своих" IP адресов
	direction := h.determineDirection(srcIP, dstIP)
	var targetIP string
	if direction == "in" {
		targetIP = dstIP
	} else {
		targetIP = srcIP
	}

	// TODO: Находим активную сессию по IP
	// session, err := h.db.FindActiveSessionByIP(targetIP)
	// if err != nil {
	//	logrus.Debugf("No active session found for IP %s: %v", targetIP, err)
	//	return
	// }

	// TODO: Implement session lookup and billing
	logrus.Debugf("NetFlow accounting for IP %s, direction %s, octets %d",
		targetIP, direction, uint64(record.Octets))
}

// determineDirection - определение направления трафика
func (h *NetFlowHandler) determineDirection(srcIP, dstIP string) string {
	// Упрощенная логика - в реальной системе нужна конфигурация
	// сетей провайдера
	src := net.ParseIP(srcIP)
	dst := net.ParseIP(dstIP)

	// Если источник - приватная сеть, а назначение - публичная, то OUT
	if isPrivateIP(src) && !isPrivateIP(dst) {
		return "out"
	}

	// Если источник - публичная, а назначение - приватная, то IN
	if !isPrivateIP(src) && isPrivateIP(dst) {
		return "in"
	}

	// По умолчанию считаем исходящим
	return "out"
}

// performAccountingForFlow - выполнение биллинга для NetFlow записи
func (h *NetFlowHandler) performAccountingForFlow(account *models.AccountWithRelations, session *models.DBIPTrafficSession, direction string, targetIP string, octets uint64) {
	// Здесь должна быть логика как в handle_cast({netflow, Dir, {H, Rec}} в iptraffic_session.erl
	// Пока упрощенная версия
	logrus.Debugf("Accounting: Account=%d, Session=%s, Direction=%s, IP=%s, Octets=%d",
		account.ID, session.SID, direction, targetIP, octets)

	// TODO: Implement full billing logic
	// 1. Get plan data and algorithms
	// 2. Call billing service
	// 3. Update session counters
	// 4. Store traffic details by class
}

// Вспомогательные функции
func intToIP(ip uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d",
		(ip>>24)&0xFF,
		(ip>>16)&0xFF,
		(ip>>8)&0xFF,
		ip&0xFF)
}

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}

	privateNetworks := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}

	for _, cidr := range privateNetworks {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(ip) {
			return true
		}
	}

	return false
}
