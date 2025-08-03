package server

import (
	"log"

	"github.com/gin-gonic/gin"

	"hips/internal/handler"
)

type Server struct {
	router        *gin.Engine
	imageHandler  *handler.ImageHandler
	healthHandler *handler.HealthHandler
}

func NewServer(imageHandler *handler.ImageHandler, healthHandler *handler.HealthHandler) *Server {
	router := gin.Default()

	router.Use(handler.CORSMiddleware())

	return &Server{
		router:        router,
		imageHandler:  imageHandler,
		healthHandler: healthHandler,
	}
}

func (s *Server) SetupRoutes() {
	// 管理端点
	s.router.POST("/admin/cache/clear", s.healthHandler.HandleCacheClear)
	s.router.POST("/admin/gc", s.healthHandler.HandleForceGC)

	// 主要路由（最后设置，避免冲突）
	s.router.GET("/*path", s.imageHandler.HandleRequest)
}

func (s *Server) Start(port string) error {
	s.SetupRoutes()

	log.Printf("hips starting on port %s", port)

	return s.router.Run(":" + port)
}
