package fcache

import (
	"encoding/binary"
	"errors"
	"os/user"
	"strings"
	"sync"
	"time"

	"github.com/boltdb/bolt"
	"github.com/mattn/go-nulltype"
)

var (
	NullString = nulltype.NullString{}
	KeyExpired = errors.New("key expired")
)

type KV struct {
	Key string
	Val string
	TTL time.Duration
}

type Cache interface {
	GetBytes(key string) ([]byte, error)
	Get(key string) (nulltype.NullString, error)
	SetBytes(key string, val []byte, ttl time.Duration) error
	Set(key, val string, ttl time.Duration) error
	TTL(key string) (time.Duration, error)
	Expire(key string, ttl time.Duration) error
	Del(key string) error
	Range() ([]*KV, error)
}

func New(filepath string) Cache {
	if strings.HasPrefix(filepath, "~") {
		u, err := user.Current()
		if err != nil {
			panic(err)
		}
		filepath = u.HomeDir + filepath[1:]
	}
	return &CacheImpl{
		filepath: filepath,
		bucket:   []byte("f-cache"),
	}
}

type CacheImpl struct {
	filepath string
	bucket   []byte
	bOnce    sync.Once
	conn     *bolt.DB
}

func (r *CacheImpl) GetBytes(key string) ([]byte, error) {
	ttl, result, err := r.getWithExpire(key)
	if err != nil {
		return nil, err
	} else if ttl < 0 {
		return nil, nil
	}
	return result, nil
}

func (r *CacheImpl) Get(key string) (nulltype.NullString, error) {
	ttl, result, err := r.getWithExpire(key)
	//fmt.Println(ttl, result, err)
	if err != nil {
		return NullString, err
	} else if ttl < 0 {
		return nulltype.NullString{}, nil
	}
	return nulltype.NullStringOf(string(result)), nil
}

func (r *CacheImpl) SetBytes(key string, val []byte, ttl time.Duration) error {
	if err := r.newConn(); err != nil {
		return err
	}

	return r.conn.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(r.bucket)
		if err != nil {
			return err
		}

		buf := make([]byte, 8+len(val))
		binary.PutVarint(buf[:8], toMillisecond(ttl))
		copy(buf[8:], val)

		//fmt.Println(key, buf[:8], buf[8:])
		return b.Put([]byte(key), buf)
	})
}

func (r *CacheImpl) Set(key, val string, ttl time.Duration) error {
	if err := r.newConn(); err != nil {
		return err
	}

	return r.conn.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(r.bucket)
		if err != nil {
			return err
		}

		buf := make([]byte, 8+len(val))
		binary.PutVarint(buf[:8], toMillisecond(ttl))
		copy(buf[8:], val)

		//fmt.Println(key, buf[:8], buf[8:])
		return b.Put([]byte(key), buf)
	})
}

func (r *CacheImpl) TTL(key string) (time.Duration, error) {
	ttl, _, err := r.getWithExpire(key)
	if err != nil {
		return -1, err
	} else if ttl < -1 {
		return -1, nil
	}
	return time.Duration(ttl) * time.Millisecond, nil
}

func (r *CacheImpl) Expire(key string, ttl time.Duration) error {
	_, result, err := r.getWithExpire(key)
	if err != nil {
		return err
	} else if ttl < -1 {
		return KeyExpired
	}
	return r.Set(key, string(result), ttl)
}

func (r *CacheImpl) Del(key string) error {
	if err := r.newConn(); err != nil {
		return err
	}

	return r.conn.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(r.bucket)
		if err != nil {
			return err
		}
		return b.Delete([]byte(key))
	})
}

func (r *CacheImpl) Range() ([]*KV, error) {
	if err := r.newConn(); err != nil {
		return nil, err
	}

	var kvs []*KV
	if err := r.conn.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(r.bucket)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, v []byte) error {
			expiredAt, err := binaryInt(v[:8])
			if err != nil {
				return err
			}
			ttl := expiredAt - int(time.Now().UnixNano()/int64(1000000))
			if ttl < 0 {
				// TODO: 删除
				return nil
			}

			kvs = append(kvs, &KV{
				Key: string(k),
				Val: string(v[8:]),
				TTL: time.Duration(ttl) * time.Millisecond,
			})
			return nil
		})
	}); err != nil {
		return nil, err
	}
	return kvs, nil
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

func (r *CacheImpl) getOriginData(key string) ([]byte, error) {
	if err := r.newConn(); err != nil {
		return nil, err
	}

	var result []byte
	if err := r.conn.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(r.bucket)
		if b == nil {
			return nil
		}

		result = b.Get([]byte(key))
		return nil
	}); err != nil {
		return nil, err
	}

	return result, nil
}

func (r *CacheImpl) getWithExpire(key string) (int, []byte, error) {
	result, err := r.getOriginData(key)
	//fmt.Println(1, result, err)
	if err != nil {
		return -1, nil, nil
	} else if result == nil {
		return -1, nil, nil
	}
	expiredAt, err := binaryInt(result[:8])
	if err != nil {
		return -1, nil, err
	}
	ttl := expiredAt - int(time.Now().UnixNano()/int64(1000000))
	if ttl < 0 {
		// 过期了
		// TODO: 删除
		return -1, nil, err
	}

	return ttl, result[8:], nil
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
