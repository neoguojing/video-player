package server

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type client struct {
	conn     *websocket.Conn
	clientID string
	windows  map[string]WindowParams
	mu       sync.Mutex
}

func (s *Server) handleWebSocket(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Errorf("Error upgrading to WebSocket: %v", err)
		return
	}
	clientID := uuid.NewString()
	client := &client{
		conn:     conn,
		clientID: clientID,
		windows:  make(map[string]WindowParams),
	}
	defer s.closeWebSocketConnection(client)

	// 启动心跳检测
	go s.heartbeat(client)

	// 设置连接关闭处理函数
	conn.SetCloseHandler(func(code int, text string) error {
		log.Infof("Client %s disconnected (code: %d, reason: %s)\n", clientID, code, text)
		// 关闭该连接所有window
		s.closeWebSocketConnection(client)
		return nil
	})

	for {
		messageType, p, err := conn.ReadMessage()
		if err != nil {
			log.WithError(err).Error("WebSocket connection closed")
			break
		}

		log.Debugf("Received message: Type=%d, Content=%s", messageType, p)

		switch messageType {
		case websocket.TextMessage:
			var msg WindowParams
			if err := json.Unmarshal(p, &msg); err != nil {
				log.WithError(err).Error("Error decoding WebSocket message")
				continue
			}
			if msg.Command == "heartbeat" {
				s.handleWebSocketHeartBeat(client, msg)
			} else {
				log.Infof("receive client message, %v", msg)
				s.handleWebSocketOperation(client, msg)
			}
		default:
			log.Infof("WebSocket message type: %d", messageType)
		}
	}
}

func (s *Server) handleWebSocketHeartBeat(c *client, params WindowParams) {
	log.Debugf("receive heartbeat from client: %v, msg: %v", c.clientID, params)
	c.conn.WriteJSON(WindowParams{Command: "heartbeat"})
}

func (s *Server) handleWebSocketOperation(c *client, params WindowParams) {
	switch params.Command {
	case "open-window":
		s.handleWebSocketOpenWindow(c, params)
	case "move-window":
		s.handleWebSocketMoveWindow(c, params)
	case "close-window":
		s.handleWebSocketCloseWindow(c, params)
	case "hide-window":
		s.handleWebSocketHideWindow(c, params)
	case "show-window":
		s.handleWebSocketShowWindow(c, params)
	case "close-all-windows":
		s.handleWebSocketCloseAllWindows(c, params)
	default:
		log.Infof("Unknown command: %s", params.Command)
	}
}

func (s *Server) handleWebSocketOpenWindow(c *client, params WindowParams) {
	log.Infof("open window: %v", params)
	c.mu.Lock()
	defer c.mu.Unlock()
	var ret Ret
	if err := s.manager.HandleOpenWindow(params); err != nil {
		ret.Code = Failed
		ret.Message = err.Error()
		ret.Data = params
		s.sendWebSocketMessage(c, ret)
		return
	}
	c.windows[params.WindowID] = params
	ret.Code = Success
	ret.Message = "success"
	ret.Data = params
	s.sendWebSocketMessage(c, ret)
}

func (s *Server) handleWebSocketMoveWindow(c *client, params WindowParams) {
	log.Infof("move window: %v", params)
	c.mu.Lock()
	defer c.mu.Unlock()
	var ret Ret
	if err := s.manager.HandleMoveWindow(params); err != nil {
		ret.Code = Failed
		ret.Message = err.Error()
		ret.Data = params
		s.sendWebSocketMessage(c, ret)
		return
	}
	c.windows[params.WindowID] = params
	ret.Code = Success
	ret.Message = "success"
	ret.Data = params
	s.sendWebSocketMessage(c, ret)
}

func (s *Server) handleWebSocketCloseWindow(c *client, params WindowParams) {
	log.Infof("close window: %v", params)
	c.mu.Lock()
	defer c.mu.Unlock()
	var ret Ret
	if err := s.manager.HandleCloseWindow(params); err != nil {
		ret.Code = Failed
		ret.Message = err.Error()
		ret.Data = params
		s.sendWebSocketMessage(c, ret)
		return
	}
	delete(c.windows, params.WindowID)
	ret.Code = Success
	ret.Message = "success"
	ret.Data = params
	s.sendWebSocketMessage(c, ret)
}

func (s *Server) handleWebSocketHideWindow(c *client, params WindowParams) {
	log.Infof("hide window: %v", params)
	c.mu.Lock()
	defer c.mu.Unlock()
	var ret Ret
	if err := s.manager.HandleHideWindow(params); err != nil {
		ret.Code = Failed
		ret.Message = err.Error()
		ret.Data = params
		s.sendWebSocketMessage(c, ret)
		return
	}
	delete(c.windows, params.WindowID)
	ret.Code = Success
	ret.Message = "success"
	ret.Data = params
	s.sendWebSocketMessage(c, ret)
}

func (s *Server) handleWebSocketShowWindow(c *client, params WindowParams) {
	log.Infof("show window: %v", params)
	c.mu.Lock()
	defer c.mu.Unlock()
	var ret Ret
	if err := s.manager.HandleShowWindow(params); err != nil {
		ret.Code = Failed
		ret.Message = err.Error()
		ret.Data = params
		s.sendWebSocketMessage(c, ret)
		return
	}
	c.windows[params.WindowID] = params
	ret.Code = Success
	ret.Message = "success"
	ret.Data = params
	s.sendWebSocketMessage(c, ret)
}

func (s *Server) handleWebSocketCloseAllWindows(c *client, params WindowParams) {
	log.Infof("close all window: %v", params)
	c.mu.Lock()
	defer c.mu.Unlock()
	var ret Ret
	if err := s.manager.HandleCloseAllWindows(); err != nil {
		ret.Code = Failed
		ret.Message = err.Error()
		ret.Data = params
		s.sendWebSocketMessage(c, ret)
		return
	}
	c.windows = make(map[string]WindowParams)
	ret.Code = Success
	ret.Message = "success"
	ret.Data = params
	s.sendWebSocketMessage(c, ret)
}

func (s *Server) sendWebSocketMessage(c *client, message interface{}) {
	if err := c.conn.WriteJSON(message); err != nil {
		log.WithError(err).Error("Error sending WebSocket message")
	}
}

func (s *Server) closeWebSocketConnection(c *client) {
	if err := c.conn.Close(); err != nil {
		log.WithError(err).Error("Error closing WebSocket connection")
	}
	log.Infof("close all windows")
	// close all windows that belongs to this conn
	if err := s.manager.HandleCloseAllWindows(); err != nil {
		return
	}
}

func (s *Server) heartbeat(c *client) {
	ticker := time.NewTicker(10 * time.Second)
	defer func() {
		ticker.Stop()
		s.closeWebSocketConnection(c)
	}()

	for {
		select {
		case <-ticker.C:
			if err := c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				log.WithError(err).Error("Error sending heartbeat")
				return
			}
		}
	}
}
