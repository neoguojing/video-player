package server

import (
	"fmt"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"videoplayer/player"
)

const (
	Success int = 0
	Failed  int = -1
)

// Server 结构体
type Server struct {
	router  *gin.Engine
	manager *WindowManager
}

// WindowParams 窗口参数结构体
type WindowParams struct {
	WSURL    string `json:"wsurl"`
	RTSPURL  string `json:"rtspurl"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	WindowID string `json:"windowID"`
	Command  string `json:"command"`
}

type Ret struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// NewServer creates a new instance of the Server struct.
func NewServer() *Server {
	return &Server{
		router:  gin.Default(),
		manager: NewWindowManager(player.NewPlayer()),
	}
}

// SetupRoutes configures the HTTP routes for the server.
func (s *Server) SetupRoutes() {
	s.router.GET("/", s.handleRoot)
	s.router.POST("/open-window", s.handleOpenWindow)
	s.router.POST("/move-window/:id", s.handleMoveWindow)
	s.router.POST("/close-window/:id", s.handleCloseWindow)
	s.router.POST("/close-all-windows", s.handleCloseWindow)
	s.router.POST("/hide-window/:id", s.handleHideWindow)
	s.router.POST("/show-window/:id", s.handleShowWindow)
	s.router.GET("/list-window", s.handleListWindow)

	// 设置 WebSocket 路由
	s.router.GET("/ws", s.handleWebSocket)
}

// Run starts the server on the specified port.
func (s *Server) Run(port int) {
	addr := fmt.Sprintf(":%d", port)
	err := s.router.Run(addr)
	if err != nil {
		log.WithError(err).Fatal("Error starting server")
	}
}

func (s *Server) StartPlayer() {
	s.manager.Run()
}
