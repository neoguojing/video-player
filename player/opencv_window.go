//go:build !sdl
// +build !sdl

package player

import (
	"fmt"
	"videoplayer/ffmpeg"
	"videoplayer/pb"

	log "github.com/sirupsen/logrus"
	"gocv.io/x/gocv"
)

const (
	WindowPropertyFlagTopmost gocv.WindowPropertyFlag = 5
	WINDOW_OPENGL             gocv.WindowFlag         = 0x00001000
)

type OpencvWindow struct {
	*gocv.Window
	Device
	Position
}

func init() {
	log.Info("running in OPENCV mode")
}

func NewOpencvWindow(pos Position, dev Device) *OpencvWindow {
	windowTitle := fmt.Sprintf("opencv video player %v", dev.ID)
	window := gocv.NewWindow(windowTitle)
	// 硬件加速支持
	window.SetWindowProperty(gocv.WindowPropertyOpenGL, WINDOW_OPENGL)
	// 隐藏菜单栏
	window.SetWindowProperty(gocv.WindowPropertyFullscreen, gocv.WindowFullscreen)
	// 窗口固定最前
	window.SetWindowProperty(WindowPropertyFlagTopmost, 1)
	// 设置窗口位置
	window.MoveWindow(pos.x, pos.y)
	window.ResizeWindow(pos.width, pos.height)
	return &OpencvWindow{
		Position: pos,
		Device:   dev,
		Window:   window,
	}
}

func (cv *OpencvWindow) GetType() string {
	return "opencv"
}

func (cv *OpencvWindow) Close() error {
	err := cv.Window.Close()
	cv.Window = nil
	return err
}

func (cv *OpencvWindow) IsOpen() bool {
	return cv.Window.IsOpen()
}

func (cv *OpencvWindow) Hide() {
	cv.Window.SetWindowProperty(WindowPropertyFlagTopmost, 0)
}

func (cv *OpencvWindow) Show() {
	cv.Window.SetWindowProperty(WindowPropertyFlagTopmost, 1)
}

func (cv *OpencvWindow) MoveWindow(x int, y int) {
	cv.Window.MoveWindow(x, y)
	cv.Position.x = x
	cv.Position.y = y
}

func (cv *OpencvWindow) ResizeWindow(width int, height int) {
	cv.Window.ResizeWindow(width, height)
	cv.Position.width = width
	cv.Position.height = height
}

func (cv *OpencvWindow) IMShow(frame *ffmpeg.VideoFrame, sei []*pb.PreviewInfo) {
	if frame != nil {
		defer frame.Free()
	}

	if frame.Mat != nil {
		cv.Window.IMShow(*frame.Mat)
	}
}

func (cv *OpencvWindow) WaitKey(delay int) int {
	return cv.Window.WaitKey(delay)
}

func (cv *OpencvWindow) GetPosition() Position {
	return cv.Position
}

func (cv *OpencvWindow) GetDevice() Device {
	return cv.Device
}

func NewWindow(pos Position, dev Device, useOpencv bool, isCUDA bool) Window {
	return NewOpencvWindow(pos, dev)
}
