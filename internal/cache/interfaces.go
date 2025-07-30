package cache

import "time"

type CacheService interface {
	Get(key string) (interface{}, bool)
	Set(key string, value interface{}, duration time.Duration)
	ItemCount() int
}
