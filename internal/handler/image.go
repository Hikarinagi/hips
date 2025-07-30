package handler

import (
	"log"
	"net/http"
	"strings"
	"time"

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
	start := time.Now()

	imagePath := strings.TrimPrefix(c.Request.URL.Path, "/")
	if imagePath == "" || imagePath == "health" {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "Ciallo～ (∠・ω< )⌒★"})
		return
	}

	params := imaging.ParseImageParams(c.Request.URL.Query())

	processedData, contentType, err := h.imageService.ProcessImageRequest(imagePath, params)
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
	c.Header("X-Process-Time", time.Since(start).String())
	c.Data(http.StatusOK, contentType, processedData)
}
