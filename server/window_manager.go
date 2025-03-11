package server

import (
	"videoplayer/player"

	log "github.com/sirupsen/logrus"
)

// WindowManager 结构体
type WindowManager struct {
	player *player.Player
}

// NewWindowManager 创建 WindowManager 实例
func NewWindowManager(player *player.Player) *WindowManager {
	return &WindowManager{
		player: player,
	}
}

func (m *WindowManager) Run() {
	m.player.Run()
}

// HandleOpenWindow 处理打开窗口的操作
func (m *WindowManager) HandleOpenWindow(windowParams WindowParams) error {
	err := make(chan error)
	// 在这里打印请求参数
	log.WithFields(log.Fields{"windowParams": windowParams}).Debug("Received request")
	m.player.CommandChan() <- player.Request{
		Type: player.PlayVideo,
		Device: player.Device{
			ID:      windowParams.WindowID,
			WSURL:   windowParams.WSURL,
			RTSPURL: windowParams.RTSPURL,
		},
		Pos: player.NewPosition(windowParams.X, windowParams.Y, windowParams.Width, windowParams.Height),
		Err: err,
	}
	return <-err
}

// HandleCloseWindow 处理关闭窗口的操作
func (m *WindowManager) HandleCloseWindow(windowParams WindowParams) error {
	err := make(chan error)
	m.player.CommandChan() <- player.Request{
		Type:   player.CloseVideo,
		Device: player.Device{ID: windowParams.WindowID},
		Err:    err,
	}
	return <-err
}

// HandleMoveWindow 处理移动窗口的操作
func (m *WindowManager) HandleMoveWindow(windowParams WindowParams) error {
	err := make(chan error)
	// Move the specified window by ID
	m.player.CommandChan() <- player.Request{
		Type: player.MoveWindow,
		Device: player.Device{
			ID: windowParams.WindowID,
		},
		Pos: player.NewPosition(windowParams.X, windowParams.Y, windowParams.Width, windowParams.Height),
		Err: err,
	}
	return <-err
}

// HandleShowWindow 处理显示窗口的操作
func (m *WindowManager) HandleShowWindow(windowParams WindowParams) error {
	err := make(chan error)
	m.player.CommandChan() <- player.Request{
		// Type: player.PlayVideo, // todo hide
		Type: player.ShowWindow, // todo hide
		Device: player.Device{
			ID:      windowParams.WindowID,
			WSURL:   windowParams.WSURL,
			RTSPURL: windowParams.RTSPURL,
		},
		Pos: player.NewPosition(windowParams.X, windowParams.Y, windowParams.Width, windowParams.Height),
		Err: err,
	}
	return <-err
}

// HandleHideWindow 处理隐藏窗口的操作
func (m *WindowManager) HandleHideWindow(windowParams WindowParams) error {
	err := make(chan error)
	m.player.CommandChan() <- player.Request{
		// Type:   player.CloseVideo, // todo hide
		Type:   player.HideWindow, // todo hide
		Device: player.Device{ID: windowParams.WindowID},
		Err:    err,
	}
	return <-err
}

// HandleCloseAllWindows 关闭所有窗口 todo
func (m *WindowManager) HandleCloseAllWindows() error {
	err := make(chan error)
	m.player.CommandChan() <- player.Request{
		Type: player.CloseAll, // todo hide
		Err:  err,
	}
	return <-err
}
