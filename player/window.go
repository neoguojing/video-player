package player

import (
	"videoplayer/ffmpeg"
	"videoplayer/pb"
)

type Device struct {
	ID      string
	WSURL   string
	RTSPURL string
}

func NewDevice(id string, wsURL, rtspURL string) Device {
	return Device{
		ID:      id,
		WSURL:   wsURL,
		RTSPURL: rtspURL,
	}
}

type Position struct {
	x, y, width, height int
}

func NewPosition(x, y, width, height int) Position {
	return Position{
		x:      x,
		y:      y,
		width:  width,
		height: height,
	}
}

type Window interface {
	Close() error
	IMShow(*ffmpeg.VideoFrame, []*pb.PreviewInfo)
	IsOpen() bool
	Hide()
	Show()
	MoveWindow(x int, y int)
	ResizeWindow(width int, height int)
	WaitKey(delay int) int
	GetPosition() Position
	GetDevice() Device
	GetType() string
}
