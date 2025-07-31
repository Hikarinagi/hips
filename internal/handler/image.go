package handler

import (
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
	path := strings.TrimPrefix(c.Request.URL.Path, "/")

	if path == "health" {
		h.healthHandler.HandleHealth(c)
		return
	}

	h.HandleImageProxy(c)
}

func (h *ImageHandler) HandleImageProxy(c *gin.Context) {
	imagePath := strings.TrimPrefix(c.Request.URL.Path, "/")
	if imagePath == "" || imagePath == "health" {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "Ciallo～ (∠・ω< )⌒★"})
		return
	}

	params := imaging.ParseImageParams(c.Request.URL.Query())

	result, err := h.imageService.ProcessImageRequestWithTiming(imagePath, params)
	if err != nil {
		log.Printf("Error processing image: %v", err)

		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "NoSuchKey") {
			c.JSON(http.StatusNotFound, gin.H{"error": "image not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "image processing failed"})
		}
		return
	}

	c.Header("Cache-Control", "public, max-age=31536000")

	c.Header("X-Total-Time", result.Timings.TotalTime.String())
	c.Header("X-Network-Time", result.Timings.NetworkTime.String())
	c.Header("X-Processing-Time", result.Timings.ProcessingTime.String())

	if result.Timings.CacheHit {
		c.Header("X-Cache", "HIT")
	} else {
		c.Header("X-Cache", "MISS")
	}

	if result.Timings.ResizeSkipped {
		c.Header("X-Resize-Skipped", "true")
	} else {
		c.Header("X-Resize-Skipped", "false")
	}

	c.Data(http.StatusOK, result.ContentType, result.Data)
}
