package filecache

import (
	"encoding/binary"
	"errors"
	"github.com/boltdb/bolt"
	"sync"
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

type CacheImpl struct {
	filepath string
	bucket   []byte
	bOnce    sync.Once
	conn     *bolt.DB
}

func (r *CacheImpl) Get(key string) (string, error) {
	result, err := r.get(key)
	if err != nil {
		return "", nil
	}
	expiredAt, err := binaryInt(result[:8])
	if err != nil {
		return "", err
	}
	ttl := expiredAt - int(time.Now().UnixNano()/int64(1000000))
	if ttl < 0 {
		// 过期了
		// TODO: 删除
		return "", err
	}

	return string(result[8:]), nil
}

func (r *CacheImpl) Set(key, val string, ttl time.Duration) error {
	if err := r.newConn(); err != nil {
		return err
	}

	return r.conn.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(r.bucket)

		buf := make([]byte, 8+len(val))
		binary.PutVarint(buf[:8], toMillisecond(ttl))
		copy(buf[8:], val)

		return b.Put([]byte(key), buf)
	})
}

func (r *CacheImpl) TTL(key string) (time.Duration, error) {
	panic("implement me")
}

func (r *CacheImpl) Expire(key string, ttl time.Duration) error {
	panic("implement me")
}

func (r *CacheImpl) Del(key string) error {
	panic("implement me")
}

func (r *CacheImpl) Range() ([]*KV, error) {
	panic("implement me")
}

func (r *CacheImpl) newConn() error {
	if r.conn == nil {
		db, err := bolt.Open(r.filepath, 0600, nil)
		if err != nil {
			return err
		}
		r.conn = db
	}
	return nil
}

func New(filepath string) *CacheImpl {
	return &CacheImpl{
		filepath: filepath,
		bucket:   []byte("f-cache"),
	}
}

func (r *CacheImpl) get(key string) ([]byte, error) {
	if err := r.newConn(); err != nil {
		return nil, err
	}

	var result []byte
	if err := r.conn.View(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(r.bucket)
		if err != nil {
			return err
		}

		result = b.Get([]byte(key))
		return nil
	}); err != nil {
		return nil, err
	}

	return result, nil
}

func toMillisecond(ttl time.Duration) int64 {
	return int64(time.Now().Add(ttl).UnixNano() / int64(1000000))
}

func binaryInt(buf []byte) (int, error) {
	x, n := binary.Varint(buf)
	if n == 0 {
		return 0, errors.New("buf too small")
	} else if n < 0 {
		return 0, errors.New("value larger than 64 bits (overflow) and -n is the number of bytes read")
	}

	return int(x), nil
}
