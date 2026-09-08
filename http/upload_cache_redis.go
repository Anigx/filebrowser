package fbhttp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const uploadLeaseTTL = 30 * time.Second

var (
	redisReleaseLease = redis.NewScript(`if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("del", KEYS[1]) else return 0 end`)
	redisRenewLease   = redis.NewScript(`if redis.call("get", KEYS[1]) == ARGV[1] then return redis.call("pexpire", KEYS[1], ARGV[2]) else return 0 end`)
)

type redisUploadCache struct{ client *redis.Client }

type redisUploadEntry struct {
	Size     int64  `json:"size"`
	ObjectID string `json:"objectID"`
}

func newRedisUploadCache(redisURL string) (*redisUploadCache, error) {
	if redisURL == "" {
		return nil, fmt.Errorf("redis URL is required")
	}
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redis URL: %w", err)
	}
	client := redis.NewClient(opts)
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}
	return &redisUploadCache{client: client}, nil
}
func (c *redisUploadCache) filePathKey(filePath string) string {
	return "filebrowser:upload:" + filePath
}
func (c *redisUploadCache) leaseKey() string { return "filebrowser:upload:lease" }
func (c *redisUploadCache) Register(filePath string, fileSize int64, objectID string, _ func() error) {
	value, err := json.Marshal(redisUploadEntry{Size: fileSize, ObjectID: objectID})
	if err == nil {
		err = c.client.Set(context.Background(), c.filePathKey(filePath), value, uploadCacheTTL).Err()
	}
	if err != nil {
		log.Printf("failed to register upload in redis cache: %v", err)
	}
}
func (c *redisUploadCache) Complete(filePath string) {
	if err := c.client.Del(context.Background(), c.filePathKey(filePath)).Err(); err != nil {
		log.Printf("failed to complete upload in redis cache: %v", err)
	}
}
func (c *redisUploadCache) Get(filePath string) (uploadCacheEntry, error) {
	result, err := c.client.Get(context.Background(), c.filePathKey(filePath)).Result()
	if errors.Is(err, redis.Nil) {
		return uploadCacheEntry{}, fmt.Errorf("no active upload found for the given path")
	}
	if err != nil {
		return uploadCacheEntry{}, fmt.Errorf("redis error: %w", err)
	}
	var entry redisUploadEntry
	if err := json.Unmarshal([]byte(result), &entry); err != nil || entry.ObjectID == "" {
		if err == nil {
			err = fmt.Errorf("missing object binding")
		}
		return uploadCacheEntry{}, fmt.Errorf("invalid upload cache entry: %w", err)
	}
	c.Touch(filePath)
	return uploadCacheEntry{size: entry.Size, objectID: entry.ObjectID}, nil
}
func (c *redisUploadCache) Touch(filePath string) {
	if err := c.client.Expire(context.Background(), c.filePathKey(filePath), uploadCacheTTL).Err(); err != nil {
		log.Printf("failed to touch upload in redis cache: %v", err)
	}
}
func (c *redisUploadCache) InvalidatePathPrefix(path string) {
	prefix := strings.TrimRight(path, "/")
	keyPrefix := c.filePathKey(prefix)
	var cursor uint64
	for {
		keys, next, err := c.client.Scan(context.Background(), cursor, keyPrefix+"*", 100).Result()
		if err != nil {
			log.Printf("failed to scan upload cache for invalidation: %v", err)
			return
		}
		for _, key := range keys {
			filePath := strings.TrimPrefix(key, "filebrowser:upload:")
			if filePath == prefix || strings.HasPrefix(filePath, prefix+"/") {
				if err := c.client.Del(context.Background(), key).Err(); err != nil {
					log.Printf("failed to invalidate upload cache entry: %v", err)
				}
			}
		}
		if next == 0 {
			return
		}
		cursor = next
	}
}
func randomLeaseToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func (c *redisUploadCache) WithLease(ctx context.Context, fn func() error) error {
	token, err := randomLeaseToken()
	if err != nil {
		return err
	}
	ok, err := c.client.SetNX(ctx, c.leaseKey(), token, uploadLeaseTTL).Result()
	if err != nil {
		return fmt.Errorf("acquire upload lease: %w", err)
	}
	if !ok {
		return errUploadLeaseHeld
	}
	defer func() {
		if _, err := redisReleaseLease.Run(context.Background(), c.client, []string{c.leaseKey()}, token).Result(); err != nil {
			log.Printf("failed to release upload lease: %v", err)
		}
	}()
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(uploadLeaseTTL / 3)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if n, err := redisRenewLease.Run(context.Background(), c.client, []string{c.leaseKey()}, token, uploadLeaseTTL.Milliseconds()).Int(); err != nil || n != 1 {
					log.Printf("failed to renew upload lease: %v", err)
				}
			}
		}
	}()
	return fn()
}
func (c *redisUploadCache) Close() { _ = c.client.Close() }
