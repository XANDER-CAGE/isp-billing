package ippool

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"netspire-go/internal/models"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

const (
	DefaultTimeout    = 300 // 5 minutes, same as Erlang ?TIMEOUT
	RedisIPPoolPrefix = "ippool:"
	RedisPoolsListKey = "ippool:pools"
	RedisStatsPrefix  = "ippool:stats:"
)

// Service handles IP pool management
// Full equivalent to mod_ippool.erl functionality
type Service struct {
	redis  *redis.Client
	logger *zap.Logger
	config Config
}

// Config holds IP pool configuration
// Equivalent to mod_ippool options in Erlang config
type Config struct {
	Timeout               int                    `yaml:"timeout"`
	DefaultPool           string                 `yaml:"default_pool"`
	UseAnotherOneFreePool bool                   `yaml:"use_another_one_free_pool"`
	Allocate              bool                   `yaml:"allocate"`
	Pools                 []models.PoolConfig    `yaml:"pools"`
	Options               map[string]interface{} `yaml:"options"`
}

// New creates a new IP pool service
func New(redisClient *redis.Client, logger *zap.Logger, config Config) *Service {
	// Set defaults like in mod_ippool.erl
	if config.Timeout == 0 {
		config.Timeout = DefaultTimeout
	}
	if config.DefaultPool == "" {
		config.DefaultPool = "main"
	}

	return &Service{
		redis:  redisClient,
		logger: logger,
		config: config,
	}
}

// Start initializes IP pools from configuration
// Equivalent to start/1 in mod_ippool.erl
func (s *Service) Start() error {
	s.logger.Info("Starting IP pool service")

	if s.config.Allocate {
		s.logger.Info("Cleaning up IP pools")
		if err := s.clearAllPools(context.Background()); err != nil {
			return fmt.Errorf("failed to clear pools: %w", err)
		}

		s.logger.Info("Allocating IP pools")
		if err := s.AllocatePools(s.config.Pools); err != nil {
			return fmt.Errorf("failed to allocate pools: %w", err)
		}
	}

	return nil
}

// AllocatePools creates IP pools from configuration
// Equivalent to allocate/1 in mod_ippool.erl
func (s *Service) AllocatePools(pools []models.PoolConfig) error {
	for _, pool := range pools {
		if err := s.addPool(pool.Name, pool.Ranges); err != nil {
			return fmt.Errorf("failed to add pool %s: %w", pool.Name, err)
		}
	}
	return nil
}

// addPool adds a single pool with IP ranges
// Equivalent to add_pool/2 in mod_ippool.erl
func (s *Service) addPool(poolName string, ranges []string) error {
	s.logger.Info("Adding IP pool", zap.String("pool", poolName), zap.Strings("ranges", ranges))

	for _, rangeStr := range ranges {
		if err := s.addRange(poolName, rangeStr); err != nil {
			return fmt.Errorf("failed to add range %s to pool %s: %w", rangeStr, poolName, err)
		}
	}
	return nil
}

// addRange adds IP range to pool
// Equivalent to add_range/2 in mod_ippool.erl
func (s *Service) addRange(poolName, rangeStr string) error {
	ips, err := s.parseIPRange(rangeStr)
	if err != nil {
		return fmt.Errorf("failed to parse IP range %s: %w", rangeStr, err)
	}

	ctx := context.Background()
	pipe := s.redis.Pipeline()

	// Add each IP to pool
	for _, ip := range ips {
		entry := &models.IPPoolEntry{
			IP:        ip,
			Pool:      poolName,
			ExpiresAt: 0, // Free
		}

		entryJSON, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("failed to marshal IP entry: %w", err)
		}

		key := fmt.Sprintf("%s%s", RedisIPPoolPrefix, ip.String())
		pipe.Set(ctx, key, entryJSON, 0)
	}

	// Add pool to pools list
	pipe.SAdd(ctx, RedisPoolsListKey, poolName)

	// Update pool stats
	statsKey := fmt.Sprintf("%sstats:%s", RedisIPPoolPrefix, poolName)
	pipe.HSet(ctx, statsKey, "total", len(ips))
	pipe.HSet(ctx, statsKey, "used", 0)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to execute Redis pipeline: %w", err)
	}

	s.logger.Info("Added IP range to pool",
		zap.String("pool", poolName),
		zap.String("range", rangeStr),
		zap.Int("count", len(ips)))

	return nil
}

// Lease allocates an IP from specified pool
// Equivalent to lease/1 in mod_ippool.erl with atomic Redis transaction
func (s *Service) Lease(poolName string) (net.IP, error) {
	if poolName == "" {
		poolName = s.config.DefaultPool
	}

	ctx := context.Background()

	// Atomic lease operation using Redis transaction
	txf := func(tx *redis.Tx) error {
		// Get all IPs in pool that are free or expired
		keys, err := tx.Keys(ctx, RedisIPPoolPrefix+"*").Result()
		if err != nil {
			return err
		}

		for _, key := range keys {
			entryJSON, err := tx.Get(ctx, key).Result()
			if err != nil {
				continue
			}

			var entry models.IPPoolEntry
			if err := json.Unmarshal([]byte(entryJSON), &entry); err != nil {
				continue
			}

			// Check if this IP belongs to requested pool and is available
			if entry.Pool == poolName && entry.IsFree() {
				// Lease this IP
				entry.LeaseIP(s.config.Timeout)

				newEntryJSON, err := json.Marshal(entry)
				if err != nil {
					continue
				}

				// Update in transaction
				_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
					pipe.Set(ctx, key, newEntryJSON, 0)
					// Update stats
					statsKey := fmt.Sprintf("%sstats:%s", RedisIPPoolPrefix, poolName)
					pipe.HIncrBy(ctx, statsKey, "used", 1)
					return nil
				})

				if err == nil {
					s.logger.Info("Leased IP from pool",
						zap.String("ip", entry.IP.String()),
						zap.String("pool", poolName),
						zap.Int64("expires_at", entry.ExpiresAt))
					return nil // Success, IP stored in entry
				}
			}
		}

		return redis.TxFailedErr // No IP found, retry
	}

	// Execute transaction with retry
	for retries := 0; retries < 5; retries++ {
		err := s.redis.Watch(ctx, txf, RedisIPPoolPrefix+"*")
		if err == nil {
			// Transaction succeeded, find the leased IP
			return s.findLeasedIP(ctx, poolName)
		}
		if err != redis.TxFailedErr {
			break
		}
		// Retry transaction
	}

	// Try alternative pool if configured
	if s.config.UseAnotherOneFreePool {
		return s.leaseFromAnyPool()
	}

	return nil, fmt.Errorf("no available IPs in pool %s", poolName)
}

// findLeasedIP finds the most recently leased IP in pool
func (s *Service) findLeasedIP(ctx context.Context, poolName string) (net.IP, error) {
	keys, err := s.redis.Keys(ctx, RedisIPPoolPrefix+"*").Result()
	if err != nil {
		return nil, err
	}

	var latestIP net.IP
	var latestTime int64

	for _, key := range keys {
		entryJSON, err := s.redis.Get(ctx, key).Result()
		if err != nil {
			continue
		}

		var entry models.IPPoolEntry
		if err := json.Unmarshal([]byte(entryJSON), &entry); err != nil {
			continue
		}

		if entry.Pool == poolName && entry.ExpiresAt > latestTime {
			latestTime = entry.ExpiresAt
			latestIP = entry.IP
		}
	}

	if latestIP != nil {
		return latestIP, nil
	}

	return nil, fmt.Errorf("failed to find leased IP")
}

// leaseFromAnyPool tries to lease from any available pool
// Equivalent to use_another_one_free_pool logic in mod_ippool.erl
func (s *Service) leaseFromAnyPool() (net.IP, error) {
	ctx := context.Background()
	pools := s.redis.SMembers(ctx, RedisPoolsListKey)
	if pools.Err() != nil {
		return nil, pools.Err()
	}

	for _, pool := range pools.Val() {
		if ip, err := s.Lease(pool); err == nil {
			s.logger.Info("Leased IP from alternative pool",
				zap.String("ip", ip.String()),
				zap.String("pool", pool))
			return ip, nil
		}
	}

	return nil, fmt.Errorf("no available IPs in any pool")
}

// Renew extends lease time for IP
// Equivalent to renew/1 in mod_ippool.erl
func (s *Service) Renew(ip net.IP) error {
	ctx := context.Background()
	key := fmt.Sprintf("%s%s", RedisIPPoolPrefix, ip.String())

	entryJSON := s.redis.Get(ctx, key)
	if entryJSON.Err() != nil {
		if entryJSON.Err() == redis.Nil {
			return fmt.Errorf("IP not found: %s", ip.String())
		}
		return fmt.Errorf("failed to get IP entry: %w", entryJSON.Err())
	}

	var entry models.IPPoolEntry
	if err := json.Unmarshal([]byte(entryJSON.Val()), &entry); err != nil {
		return fmt.Errorf("failed to unmarshal IP entry: %w", err)
	}

	// Renew lease
	entry.LeaseIP(s.config.Timeout)

	newEntryJSON, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal updated entry: %w", err)
	}

	err = s.redis.Set(ctx, key, newEntryJSON, 0).Err()
	if err != nil {
		return fmt.Errorf("failed to update IP entry: %w", err)
	}

	s.logger.Info("Renewed IP lease",
		zap.String("ip", ip.String()),
		zap.Int64("expires_at", entry.ExpiresAt))
	return nil
}

// Release frees IP back to pool
// Equivalent to release_framed_ip/1 in mod_ippool.erl
func (s *Service) Release(ip net.IP) error {
	ctx := context.Background()
	key := fmt.Sprintf("%s%s", RedisIPPoolPrefix, ip.String())

	entryJSON := s.redis.Get(ctx, key)
	if entryJSON.Err() != nil {
		if entryJSON.Err() == redis.Nil {
			// IP not found, ignore like Erlang version does
			s.logger.Debug("IP not found for release, ignoring", zap.String("ip", ip.String()))
			return nil
		}
		return fmt.Errorf("failed to get IP entry: %w", entryJSON.Err())
	}

	var entry models.IPPoolEntry
	if err := json.Unmarshal([]byte(entryJSON.Val()), &entry); err != nil {
		return fmt.Errorf("failed to unmarshal IP entry: %w", err)
	}

	// Release IP
	entry.ReleaseIP()

	newEntryJSON, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal updated entry: %w", err)
	}

	// Update entry and stats atomically
	pipe := s.redis.Pipeline()
	pipe.Set(ctx, key, newEntryJSON, 0)

	// Update stats
	statsKey := fmt.Sprintf("%sstats:%s", RedisIPPoolPrefix, entry.Pool)
	pipe.HIncrBy(ctx, statsKey, "used", -1)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update IP entry: %w", err)
	}

	s.logger.Info("Released IP",
		zap.String("ip", ip.String()),
		zap.String("pool", entry.Pool))
	return nil
}

// Info returns all IP pool entries
// Equivalent to info/0 in mod_ippool.erl
func (s *Service) Info() ([]models.IPPoolEntry, error) {
	ctx := context.Background()
	keys := s.redis.Keys(ctx, RedisIPPoolPrefix+"*")
	if keys.Err() != nil {
		return nil, fmt.Errorf("failed to get pool keys: %w", keys.Err())
	}

	var entries []models.IPPoolEntry
	for _, key := range keys.Val() {
		// Skip stats keys
		if strings.Contains(key, "stats:") || key == RedisPoolsListKey {
			continue
		}

		entryJSON := s.redis.Get(ctx, key)
		if entryJSON.Err() != nil {
			continue
		}

		var entry models.IPPoolEntry
		if err := json.Unmarshal([]byte(entryJSON.Val()), &entry); err != nil {
			continue
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// GetStats returns statistics for all pools or specific pool
func (s *Service) GetStats(poolName string) ([]models.IPPoolStats, error) {
	ctx := context.Background()
	var stats []models.IPPoolStats

	if poolName != "" {
		// Get stats for specific pool
		stat, err := s.getPoolStats(ctx, poolName)
		if err != nil {
			return nil, err
		}
		stats = append(stats, *stat)
	} else {
		// Get stats for all pools
		pools := s.redis.SMembers(ctx, RedisPoolsListKey)
		if pools.Err() != nil {
			return nil, pools.Err()
		}

		for _, pool := range pools.Val() {
			stat, err := s.getPoolStats(ctx, pool)
			if err != nil {
				s.logger.Warn("Failed to get stats for pool", zap.String("pool", pool), zap.Error(err))
				continue
			}
			stats = append(stats, *stat)
		}
	}

	return stats, nil
}

// getPoolStats calculates statistics for a single pool
func (s *Service) getPoolStats(ctx context.Context, poolName string) (*models.IPPoolStats, error) {
	// Get basic stats from Redis
	statsKey := fmt.Sprintf("%sstats:%s", RedisIPPoolPrefix, poolName)
	statsMap := s.redis.HGetAll(ctx, statsKey)
	if statsMap.Err() != nil {
		return nil, statsMap.Err()
	}

	result := statsMap.Val()
	total, _ := strconv.Atoi(result["total"])

	// Calculate real-time stats by checking actual IPs
	realUsed, expired := s.calculateRealStats(ctx, poolName)

	return &models.IPPoolStats{
		PoolName:   poolName,
		TotalIPs:   total,
		UsedIPs:    realUsed,
		FreeIPs:    total - realUsed,
		ExpiredIPs: expired,
	}, nil
}

// calculateRealStats calculates real-time statistics by examining IPs
func (s *Service) calculateRealStats(ctx context.Context, poolName string) (used, expired int) {
	keys, err := s.redis.Keys(ctx, RedisIPPoolPrefix+"*").Result()
	if err != nil {
		return 0, 0
	}

	now := time.Now().Unix()

	for _, key := range keys {
		// Skip stats keys
		if strings.Contains(key, "stats:") || key == RedisPoolsListKey {
			continue
		}

		entryJSON, err := s.redis.Get(ctx, key).Result()
		if err != nil {
			continue
		}

		var entry models.IPPoolEntry
		if err := json.Unmarshal([]byte(entryJSON), &entry); err != nil {
			continue
		}

		if entry.Pool != poolName {
			continue
		}

		if entry.ExpiresAt > 0 {
			if entry.ExpiresAt <= now {
				expired++
			} else {
				used++
			}
		}
	}

	return used, expired
}

// clearAllPools removes all IP pool entries
func (s *Service) clearAllPools(ctx context.Context) error {
	keys := s.redis.Keys(ctx, RedisIPPoolPrefix+"*")
	if keys.Err() != nil {
		return keys.Err()
	}

	allKeys := keys.Val()
	allKeys = append(allKeys, RedisPoolsListKey)

	if len(allKeys) > 0 {
		return s.redis.Del(ctx, allKeys...).Err()
	}

	return nil
}

// parseIPRange parses IP range string (CIDR, range, or single IP)
// Equivalent to iplib:range2list/1 functionality from Erlang
func (s *Service) parseIPRange(rangeStr string) ([]net.IP, error) {
	// Handle CIDR notation
	if strings.Contains(rangeStr, "/") {
		return s.parseCIDR(rangeStr)
	}

	// Handle range notation (192.168.1.10-192.168.1.20)
	if strings.Contains(rangeStr, "-") {
		return s.parseRange(rangeStr)
	}

	// Single IP
	ip := net.ParseIP(strings.TrimSpace(rangeStr))
	if ip == nil {
		return nil, fmt.Errorf("invalid IP address: %s", rangeStr)
	}

	return []net.IP{ip}, nil
}

// parseCIDR parses CIDR notation
func (s *Service) parseCIDR(cidr string) ([]net.IP, error) {
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR: %w", err)
	}

	var ips []net.IP
	for ip := ip.Mask(ipNet.Mask); ipNet.Contains(ip); s.incIP(ip) {
		ips = append(ips, make(net.IP, len(ip)))
		copy(ips[len(ips)-1], ip)
	}

	// Remove network and broadcast addresses for /24 and smaller
	ones, _ := ipNet.Mask.Size()
	if ones >= 24 && len(ips) > 2 {
		ips = ips[1 : len(ips)-1]
	}

	return ips, nil
}

// parseRange parses IP range (start-end format)
func (s *Service) parseRange(rangeStr string) ([]net.IP, error) {
	parts := strings.Split(rangeStr, "-")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid range format: %s", rangeStr)
	}

	start := net.ParseIP(strings.TrimSpace(parts[0]))
	end := net.ParseIP(strings.TrimSpace(parts[1]))

	if start == nil || end == nil {
		return nil, fmt.Errorf("invalid IP addresses in range: %s", rangeStr)
	}

	return s.generateIPRange(start, end)
}

// generateIPRange generates all IPs between start and end (inclusive)
func (s *Service) generateIPRange(start, end net.IP) ([]net.IP, error) {
	var ips []net.IP

	current := make(net.IP, len(start))
	copy(current, start)

	for {
		ips = append(ips, make(net.IP, len(current)))
		copy(ips[len(ips)-1], current)

		if current.Equal(end) {
			break
		}

		s.incIP(current)

		// Safety check to prevent infinite loops
		if len(ips) > 65536 {
			return nil, fmt.Errorf("IP range too large")
		}
	}

	return ips, nil
}

// incIP increments IP address by 1
func (s *Service) incIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

// CleanupExpiredIPs removes expired IP leases (maintenance function)
func (s *Service) CleanupExpiredIPs() error {
	ctx := context.Background()
	keys, err := s.redis.Keys(ctx, RedisIPPoolPrefix+"*").Result()
	if err != nil {
		return err
	}

	now := time.Now().Unix()
	cleaned := 0

	for _, key := range keys {
		// Skip stats keys
		if strings.Contains(key, "stats:") || key == RedisPoolsListKey {
			continue
		}

		entryJSON, err := s.redis.Get(ctx, key).Result()
		if err != nil {
			continue
		}

		var entry models.IPPoolEntry
		if err := json.Unmarshal([]byte(entryJSON), &entry); err != nil {
			continue
		}

		if entry.ExpiresAt > 0 && entry.ExpiresAt <= now {
			// Release expired IP
			entry.ReleaseIP()

			newEntryJSON, err := json.Marshal(entry)
			if err != nil {
				continue
			}

			s.redis.Set(ctx, key, newEntryJSON, 0)
			cleaned++
		}
	}

	if cleaned > 0 {
		s.logger.Info("Cleaned up expired IP leases", zap.Int("count", cleaned))
	}

	return nil
}
