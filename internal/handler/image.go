package handler

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"hips/internal/service"
	"hips/pkg/imaging"
)

type ImageHandler struct {
	imageService  service.ImageService
	healthHandler *HealthHandler
}

func NewImageHandler(imageService service.ImageService, healthHandler *HealthHandler) *ImageHandler {
	return &ImageHandler{
		imageService:  imageService,
		healthHandler: healthHandler,
	}
}

func (h *ImageHandler) HandleRequest(c *gin.Context) {
	imagePath := strings.TrimPrefix(c.Request.URL.Path, "/")

	if imagePath == "health" {
		h.healthHandler.HandleHealth(c)
		return
	}

	h.HandleImageProxy(c, imagePath)
}

func (h *ImageHandler) HandleImageProxy(c *gin.Context, imagePath string) {
	if imagePath == "" {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "Ciallo～ (∠・ω< )⌒★"})
		return
	}

	params := imaging.ParseImageParams(c.Request.URL.Query())

	result, err := h.imageService.ProcessImageRequest(imagePath, params)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "NoSuchKey") {
			c.JSON(http.StatusNotFound, gin.H{"error": "image not found"})
		} else {
			log.Printf("Error processing image: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "image processing failed"})
		}
		return
	}

	c.Header("Cache-Control", "public, max-age=31536000")

	c.Header("X-Total-Time", result.Timings.TotalTime.String())
	c.Header("X-Network-Time", result.Timings.NetworkTime.String())
	c.Header("X-Processing-Time", result.Timings.ProcessingTime.String())

	if result.CacheInfo != nil {
		c.Header("X-Cache-Level", result.CacheInfo.Level.String())
		if result.CacheInfo.Hit {
			c.Header("X-Cache", "HIT")
			c.Header("X-Cache-Retrieve-Time", result.CacheInfo.RetrieveTime.String())
			if result.CacheInfo.Size > 0 {
				c.Header("X-Cache-Size", fmt.Sprintf("%d", result.CacheInfo.Size))
			}
		} else {
			c.Header("X-Cache", "MISS")
		}
	} else {
		if result.Timings.CacheHit {
			c.Header("X-Cache", "HIT")
		} else {
			c.Header("X-Cache", "MISS")
		}
	}

	if result.Timings.ResizeSkipped {
		c.Header("X-Resize-Skipped", "true")
	} else {
		c.Header("X-Resize-Skipped", "false")
	}

	c.Data(http.StatusOK, result.ContentType, result.Data)
}
