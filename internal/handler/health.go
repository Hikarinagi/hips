package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"hips/internal/cache"
)

type HealthHandler struct {
	cache cache.CacheService
}

func NewHealthHandler(cacheService cache.CacheService) *HealthHandler {
	return &HealthHandler{
		cache: cacheService,
	}
}

func (h *HealthHandler) HandleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":      "healthy",
		"timestamp":   time.Now().Unix(),
		"cache_items": h.cache.ItemCount(),
	})
}
