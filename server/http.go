package server

import (
	"fmt"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"net/http"
)

// handleRoot handles requests to the root endpoint.
func (s *Server) handleRoot(c *gin.Context) {
	ret := Ret{
		Code:    Success,
		Message: "Welcome to the server!",
		Data:    nil,
	}

	c.JSON(http.StatusOK, ret)
}

// handleOpenWindow handles requests to open a new window.
func (s *Server) handleOpenWindow(c *gin.Context) {
	var windowParams WindowParams
	var ret Ret
	if err := c.BindJSON(&windowParams); err != nil {
		log.Error(err)
		ret = Ret{
			Code:    Failed,
			Message: fmt.Sprintf("Error parsing request: %s", err.Error()),
		}
		c.JSON(http.StatusBadRequest, ret)
		return
	}
	ret.Data = windowParams
	// 在这里打印请求参数
	log.WithFields(log.Fields{"windowParams": windowParams}).Debug("Received request")
	if err := s.manager.HandleOpenWindow(windowParams); err != nil {
		ret.Code = Failed
		ret.Message = err.Error()
		c.JSON(http.StatusOK, ret)
		return
	}
	ret.Code = Success
	ret.Message = "success"
	c.JSON(http.StatusOK, ret)
}

// handleCloseWindow handles requests to close a window by ID.
func (s *Server) handleCloseWindow(c *gin.Context) {
	var ret Ret
	var windowParams WindowParams
	id := c.Param("id")
	windowParams.WindowID = id
	if err := s.manager.HandleCloseWindow(windowParams); err != nil {
		ret.Code = Failed
		ret.Message = err.Error()
		c.JSON(http.StatusOK, ret)
		return
	}
	ret.Code = Success
	ret.Message = "success"
	c.JSON(http.StatusOK, ret)
	return

}

// handleHideWindow handles requests to hide a window by ID.
func (s *Server) handleHideWindow(c *gin.Context) {
	var ret Ret
	var windowParams WindowParams
	id := c.Param("id")
	windowParams.WindowID = id
	if err := s.manager.HandleHideWindow(windowParams); err != nil {
		ret.Code = Failed
		ret.Message = err.Error()
		c.JSON(http.StatusOK, ret)
		return
	}
	ret.Code = Success
	ret.Message = "success"
	c.JSON(http.StatusOK, ret)
	return

}

// handleShowWindow handles requests to show a window by ID.
func (s *Server) handleShowWindow(c *gin.Context) {
	var ret Ret
	var windowParams WindowParams
	id := c.Param("id")
	windowParams.WindowID = id
	if err := s.manager.HandleShowWindow(windowParams); err != nil {
		ret.Code = Failed
		ret.Message = err.Error()
		c.JSON(http.StatusOK, ret)
		return
	}
	ret.Code = Success
	ret.Message = "success"
	ret.Data = windowParams
	c.JSON(http.StatusOK, ret)
	return
}

// handleCloseAllWindows handles requests to close all windows.
func (s *Server) handleCloseAllWindows(c *gin.Context) {
	var ret Ret
	if err := s.manager.HandleCloseAllWindows(); err != nil {
		ret.Code = Failed
		ret.Message = err.Error()
		c.JSON(http.StatusOK, ret)
		return
	}
	ret.Code = Success
	ret.Message = "success"
	c.JSON(http.StatusOK, ret)
	return
}

// handleListWindow handles requests to list all windows.
func (s *Server) handleListWindow(c *gin.Context) {
	ret := Ret{}
	ret = Ret{
		Code:    Success,
		Message: "Success",
	}
	c.JSON(http.StatusOK, ret)
	return

}

// handleMoveWindow handles requests to move a window by ID.
func (s *Server) handleMoveWindow(c *gin.Context) {
	var ret Ret
	var windowParams WindowParams
	id := c.Param("id")
	if err := c.BindJSON(&windowParams); err != nil {
		log.Error(err)
		ret = Ret{
			Code:    Failed,
			Message: fmt.Sprintf("Error parsing request: %s", err.Error()),
		}
		c.JSON(http.StatusBadRequest, ret)
		return
	}
	windowParams.WindowID = id
	if err := s.manager.HandleMoveWindow(windowParams); err != nil {
		ret.Code = Failed
		ret.Message = err.Error()
		c.JSON(http.StatusOK, ret)
		return
	}
	ret.Code = Success
	ret.Message = "success"
	ret.Data = windowParams
	c.JSON(http.StatusOK, ret)
	return
}
