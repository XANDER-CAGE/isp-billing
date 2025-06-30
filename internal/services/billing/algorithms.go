package billing

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strconv"
	"time"

	"netspire-go/internal/models"
)

// BillingAlgorithm interface for all billing algorithms
type BillingAlgorithm interface {
	Authorize(currency int, balance float64, planData map[string]interface{}) (*models.BillingResult, error)
	Account(currency int, planData map[string]interface{}, sessionData map[string]interface{}, direction string, targetIP string, octets uint64) (*models.BillingResult, error)
}

// PrepaidAlgorithm implements the prepaid billing algorithm
type PrepaidAlgorithm struct{}

func NewPrepaidAlgorithm() *PrepaidAlgorithm {
	return &PrepaidAlgorithm{}
}

func (a *PrepaidAlgorithm) Authorize(currency int, balance float64, planData map[string]interface{}) (*models.BillingResult, error) {
	// Get credit from plan data
	credit := getFloatFromPlanData(planData, "CREDIT", 0.0)

	// Get default shaper
	defaultShaper := getStringFromPlanData(planData, "SHAPER", "")

	// Check access intervals
	accessResult := checkAccessIntervals(planData, defaultShaper)
	if accessResult.Decision != "accept" {
		return &models.BillingResult{
			Decision: "reject",
			Reason:   "time_of_day",
		}, nil
	}

	// Check balance + credit
	if balance+credit >= 0 {
		replies := []models.RADIUSReply{}
		if accessResult.Shaper != "" {
			replies = append(replies, models.RADIUSReply{
				Name:  "Netspire-Shapers",
				Value: accessResult.Shaper,
			})
		}

		return &models.BillingResult{
			Decision: "accept",
			Replies:  replies,
		}, nil
	}

	return &models.BillingResult{
		Decision: "reject",
		Reason:   "low_balance",
	}, nil
}

func (a *PrepaidAlgorithm) Account(currency int, planData map[string]interface{}, sessionData map[string]interface{}, direction string, targetIP string, octets uint64) (*models.BillingResult, error) {
	// Full prepaid accounting implementation like in Erlang
	now := time.Now()
	todaySeconds := now.Hour()*3600 + now.Minute()*60 + now.Second()

	// Get intervals from plan data
	intervals, ok := planData["INTERVALS"].([]interface{})
	if !ok || len(intervals) == 0 {
		// No intervals defined, free traffic
		class := classifyTraffic(targetIP)
		return &models.BillingResult{
			Decision:     "accept",
			Amount:       0.0,
			TrafficClass: class,
			PlanData:     planData,
		}, nil
	}

	// Find current interval
	var currentPrices map[string]interface{}
	for _, interval := range intervals {
		intervalData, ok := interval.([]interface{})
		if !ok || len(intervalData) < 2 {
			continue
		}

		boundary, ok := intervalData[0].(float64)
		if !ok {
			continue
		}

		if float64(todaySeconds) < boundary {
			prices, ok := intervalData[1].(map[string]interface{})
			if ok {
				currentPrices = prices
				break
			}
		}
	}

	if currentPrices == nil {
		// No applicable interval found
		class := classifyTraffic(targetIP)
		return &models.BillingResult{
			Decision:     "accept",
			Amount:       0.0,
			TrafficClass: class,
			PlanData:     planData,
		}, nil
	}

	// Classify traffic
	class := classifyTraffic(targetIP)

	// Get class prices
	classPrices, ok := currentPrices[class]
	if !ok {
		// No prices for this class
		return &models.BillingResult{
			Decision:     "accept",
			Amount:       0.0,
			TrafficClass: class,
			PlanData:     planData,
		}, nil
	}

	// Extract prices for currency
	var inPrice, outPrice float64
	switch prices := classPrices.(type) {
	case []interface{}:
		// [[currency, in_price, out_price], ...] format
		for _, priceData := range prices {
			priceArray, ok := priceData.([]interface{})
			if !ok || len(priceArray) < 3 {
				continue
			}

			curr, ok := priceArray[0].(float64)
			if !ok || int(curr) != currency {
				continue
			}

			if in, ok := priceArray[1].(float64); ok {
				inPrice = in
			}
			if out, ok := priceArray[2].(float64); ok {
				outPrice = out
			}
			break
		}
	case map[string]interface{}:
		// {in: price, out: price} format
		if in, ok := prices["in"].(float64); ok {
			inPrice = in
		}
		if out, ok := prices["out"].(float64); ok {
			outPrice = out
		}
	}

	// Get current price based on direction
	var price float64
	if direction == "in" {
		price = inPrice
	} else {
		price = outPrice
	}

	if price == 0 {
		// Free traffic
		return &models.BillingResult{
			Decision:     "accept",
			Amount:       0.0,
			TrafficClass: class,
			PlanData:     planData,
		}, nil
	}

	// Get prepaid counter
	linkName := fmt.Sprintf("PREPAID_%s_%s", class, direction)
	counterName := getStringFromPlanData(planData, linkName, "PREPAID")
	prepaidBytes := getFloatFromPlanData(planData, counterName, 0.0)

	// Calculate overlimit
	payableOctets, remainingPrepaid := calculateOverlimit(octets, uint64(prepaidBytes))

	// Calculate amount
	amount := price * float64(payableOctets) / (1024 * 1024)

	// Update plan data if prepaid changed
	newPlanData := make(map[string]interface{})
	for k, v := range planData {
		newPlanData[k] = v
	}

	if remainingPrepaid != uint64(prepaidBytes) {
		newPlanData[counterName] = float64(remainingPrepaid)
	}

	return &models.BillingResult{
		Decision:     "accept",
		Amount:       amount,
		TrafficClass: class,
		PlanData:     newPlanData,
	}, nil
}

// LimitedPrepaidAlgorithm implements limited prepaid billing
type LimitedPrepaidAlgorithm struct{}

func NewLimitedPrepaidAlgorithm() *LimitedPrepaidAlgorithm {
	return &LimitedPrepaidAlgorithm{}
}

func (a *LimitedPrepaidAlgorithm) Authorize(currency int, balance float64, planData map[string]interface{}) (*models.BillingResult, error) {
	// Get credit and prepaid from plan data
	credit := getFloatFromPlanData(planData, "CREDIT", 0.0)
	prepaid := getFloatFromPlanData(planData, "PREPAID", 0.0)

	// Get default shaper
	defaultShaper := getStringFromPlanData(planData, "SHAPER", "")

	// Check access intervals
	accessResult := checkAccessIntervals(planData, defaultShaper)
	if accessResult.Decision != "accept" {
		return &models.BillingResult{
			Decision: "reject",
			Reason:   "time_of_day",
		}, nil
	}

	// Check balance + credit
	if balance+credit >= 0 {
		// Also check prepaid
		if prepaid > 0 {
			replies := []models.RADIUSReply{}
			if accessResult.Shaper != "" {
				replies = append(replies, models.RADIUSReply{
					Name:  "Netspire-Shapers",
					Value: accessResult.Shaper,
				})
			}

			return &models.BillingResult{
				Decision: "accept",
				Replies:  replies,
			}, nil
		}

		return &models.BillingResult{
			Decision: "reject",
			Reason:   "low_balance",
		}, nil
	}

	return &models.BillingResult{
		Decision: "reject",
		Reason:   "low_balance",
	}, nil
}

func (a *LimitedPrepaidAlgorithm) Account(currency int, planData map[string]interface{}, sessionData map[string]interface{}, direction string, targetIP string, octets uint64) (*models.BillingResult, error) {
	// Use the same accounting as PrepaidAlgorithm
	prepaidAlgo := NewPrepaidAlgorithm()
	return prepaidAlgo.Account(currency, planData, sessionData, direction, targetIP, octets)
}

// OnAuthAlgorithm implements "always accept" billing
type OnAuthAlgorithm struct{}

func NewOnAuthAlgorithm() *OnAuthAlgorithm {
	return &OnAuthAlgorithm{}
}

func (a *OnAuthAlgorithm) Authorize(currency int, balance float64, planData map[string]interface{}) (*models.BillingResult, error) {
	// Get default shaper
	defaultShaper := getStringFromPlanData(planData, "SHAPER", "")

	// Check access intervals
	accessResult := checkAccessIntervals(planData, defaultShaper)
	if accessResult.Decision != "accept" {
		return &models.BillingResult{
			Decision: "reject",
			Reason:   "time_of_day",
		}, nil
	}

	replies := []models.RADIUSReply{}
	if accessResult.Shaper != "" {
		replies = append(replies, models.RADIUSReply{
			Name:  "Netspire-Shapers",
			Value: accessResult.Shaper,
		})
	}

	return &models.BillingResult{
		Decision: "accept",
		Replies:  replies,
	}, nil
}

func (a *OnAuthAlgorithm) Account(currency int, planData map[string]interface{}, sessionData map[string]interface{}, direction string, targetIP string, octets uint64) (*models.BillingResult, error) {
	// No charging for on_auth
	class := classifyTraffic(targetIP)
	return &models.BillingResult{
		Decision:     "accept",
		Amount:       0.0,
		TrafficClass: class,
		PlanData:     planData,
	}, nil
}

// NoOverlimitAlgorithm implements no-overlimit billing
type NoOverlimitAlgorithm struct{}

func NewNoOverlimitAlgorithm() *NoOverlimitAlgorithm {
	return &NoOverlimitAlgorithm{}
}

func (a *NoOverlimitAlgorithm) Authorize(currency int, balance float64, planData map[string]interface{}) (*models.BillingResult, error) {
	// Get credit from plan data
	credit := getFloatFromPlanData(planData, "CREDIT", 0.0)

	// Get default shaper and drop speed
	defaultShaper := getStringFromPlanData(planData, "SHAPER", "")
	dropSpeed := getFloatFromPlanData(planData, "DROP_SPEED", 0.0)

	// Check access intervals
	accessResult := checkAccessIntervals(planData, defaultShaper)
	if accessResult.Decision != "accept" {
		return &models.BillingResult{
			Decision: "reject",
			Reason:   "time_of_day",
		}, nil
	}

	// Check balance + credit
	if balance+credit >= 0 {
		replies := []models.RADIUSReply{}

		if dropSpeed == 1 {
			// Use default shaper when dropped
			if defaultShaper != "" {
				replies = append(replies, models.RADIUSReply{
					Name:  "Netspire-Shapers",
					Value: defaultShaper,
				})
			}
		} else {
			// Use interval shaper
			if accessResult.Shaper != "" {
				replies = append(replies, models.RADIUSReply{
					Name:  "Netspire-Shapers",
					Value: accessResult.Shaper,
				})
			}
		}

		return &models.BillingResult{
			Decision: "accept",
			Replies:  replies,
		}, nil
	}

	return &models.BillingResult{
		Decision: "reject",
		Reason:   "low_balance",
	}, nil
}

func (a *NoOverlimitAlgorithm) Account(currency int, planData map[string]interface{}, sessionData map[string]interface{}, direction string, targetIP string, octets uint64) (*models.BillingResult, error) {
	// Calculate using prepaid algorithm
	prepaidAlgo := NewPrepaidAlgorithm()
	result, err := prepaidAlgo.Account(currency, planData, sessionData, direction, targetIP, octets)
	if err != nil {
		return nil, err
	}

	// If amount > 0, set DROP_SPEED and zero amount
	if result.Amount > 0 {
		newPlanData := make(map[string]interface{})
		for k, v := range result.PlanData {
			newPlanData[k] = v
		}
		newPlanData["DROP_SPEED"] = 1.0

		return &models.BillingResult{
			Decision:     "accept",
			Amount:       0.0,
			TrafficClass: result.TrafficClass,
			PlanData:     newPlanData,
		}, nil
	}

	return result, nil
}

// AccessResult represents the result of access interval checking
type AccessResult struct {
	Decision string
	Shaper   string
}

// checkAccessIntervals checks if access is allowed based on time intervals
func checkAccessIntervals(planData map[string]interface{}, defaultShaper string) *AccessResult {
	accessIntervals, ok := planData["ACCESS_INTERVALS"].([]interface{})
	if !ok || len(accessIntervals) == 0 {
		return &AccessResult{
			Decision: "accept",
			Shaper:   defaultShaper,
		}
	}

	now := time.Now()
	todaySeconds := now.Hour()*3600 + now.Minute()*60 + now.Second()

	for _, interval := range accessIntervals {
		intervalData, ok := interval.([]interface{})
		if !ok || len(intervalData) < 2 {
			continue
		}

		boundary, ok := intervalData[0].(float64)
		if !ok {
			continue
		}

		if float64(todaySeconds) < boundary {
			access, ok := intervalData[1].(string)
			if !ok {
				continue
			}

			var shaper string
			if len(intervalData) > 2 {
				if s, ok := intervalData[2].(string); ok {
					shaper = s
				} else {
					shaper = defaultShaper
				}
			} else {
				shaper = defaultShaper
			}

			if access == "accept" {
				return &AccessResult{
					Decision: "accept",
					Shaper:   shaper,
				}
			} else {
				return &AccessResult{
					Decision: "reject",
					Shaper:   "",
				}
			}
		}
	}

	// Default to reject if no matching interval
	return &AccessResult{
		Decision: "reject",
		Shaper:   "",
	}
}

// TrafficClassifier defines traffic classification rules
type TrafficClassifier struct {
	// Define network ranges for different classes
	LocalNetworks []*net.IPNet
	CDNNetworks   []*net.IPNet
}

var defaultClassifier *TrafficClassifier

func init() {
	defaultClassifier = &TrafficClassifier{}

	// Initialize common local/CDN networks
	// Local networks (RFC 1918)
	localCIDRs := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}

	for _, cidr := range localCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			log.Printf("Failed to parse local CIDR %s: %v", cidr, err)
			continue
		}
		defaultClassifier.LocalNetworks = append(defaultClassifier.LocalNetworks, network)
	}

	// Add common CDN networks (simplified)
	cdnCIDRs := []string{
		"8.8.8.0/24",      // Google DNS
		"1.1.1.0/24",      // Cloudflare DNS
		"208.67.222.0/24", // OpenDNS
	}

	for _, cidr := range cdnCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			log.Printf("Failed to parse CDN CIDR %s: %v", cidr, err)
			continue
		}
		defaultClassifier.CDNNetworks = append(defaultClassifier.CDNNetworks, network)
	}
}

// classifyTraffic classifies an IP address into traffic class
func classifyTraffic(targetIP string) string {
	ip := net.ParseIP(targetIP)
	if ip == nil {
		return "internet"
	}

	// Check local networks
	for _, network := range defaultClassifier.LocalNetworks {
		if network.Contains(ip) {
			return "local"
		}
	}

	// Check CDN networks
	for _, network := range defaultClassifier.CDNNetworks {
		if network.Contains(ip) {
			return "cdn"
		}
	}

	// Default to internet
	return "internet"
}

// calculateOverlimit calculates overlimit bytes and remaining prepaid
func calculateOverlimit(octets uint64, limit uint64) (payableOctets uint64, remainingLimit uint64) {
	if octets <= limit {
		return 0, limit - octets
	}
	return octets - limit, 0
}

// Helper functions for plan data extraction
func getFloatFromPlanData(planData map[string]interface{}, key string, defaultValue float64) float64 {
	if value, exists := planData[key]; exists {
		switch v := value.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		case string:
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				return f
			}
		case json.Number:
			if f, err := v.Float64(); err == nil {
				return f
			}
		}
	}
	return defaultValue
}

func getStringFromPlanData(planData map[string]interface{}, key string, defaultValue string) string {
	if value, exists := planData[key]; exists {
		switch v := value.(type) {
		case string:
			return v
		case []byte:
			return string(v)
		}
	}
	return defaultValue
}
