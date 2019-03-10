package filecache

import (
	"time"
)

type KV struct {
	Key string
	Val string
	TTL time.Duration
}

type Cache interface {
	Get(key string) (string, error)
	Set(key, val string, ttl time.Duration) error
	TTL(key string) (time.Duration, error)
	Expire(key string, ttl time.Duration) error
	Del(key string) error
	Range() ([]*KV, error)
}
