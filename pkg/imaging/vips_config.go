package imaging

/*
#cgo pkg-config: vips
#include <vips/vips.h>
*/
import "C"
import (
	"log"
	"runtime"
)

// ConfigureVips 配置libvips的并发和缓存参数
func ConfigureVips(concurrency, cacheSize, cacheMemMB int) {
	if concurrency <= 0 {
		concurrency = runtime.NumCPU() // 使用所有CPU核心
	}
	C.vips_concurrency_set(C.int(concurrency))

	if cacheSize <= 0 {
		cacheSize = 300
	}
	C.vips_cache_set_max(C.int(cacheSize))

	if cacheMemMB <= 0 {
		cacheMemMB = 512
	}
	C.vips_cache_set_max_mem(C.size_t(cacheMemMB * 1024 * 1024))

	log.Printf("libvips configured - concurrency: %d, cache_max: %d, cache_size: %d",
		concurrency, cacheSize, cacheMemMB)
}

// GetVipsInfo 获取libvips当前配置信息
func GetVipsInfo() map[string]interface{} {
	return map[string]interface{}{
		"concurrency": int(C.vips_concurrency_get()),
		"cache_max":   int(C.vips_cache_get_max()),
		"cache_size":  int(C.vips_cache_get_size()),
	}
}
