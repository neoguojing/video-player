//go:build sdl
// +build sdl

package player

// #include <SDL2/SDL_pixels.h>
import "C"
import (
	"os"
	"sync"
	"videoplayer/ffmpeg"
	"videoplayer/pb"

	log "github.com/sirupsen/logrus"
	"github.com/veandco/go-sdl2/sdl"
	"github.com/veandco/go-sdl2/ttf"
)

func init() {
	log.Info("running in SDL mode")
	err := sdl.Init(sdl.INIT_EVERYTHING)
	if err != nil {
		log.Errorf("Failed to initialize SDL: %v, %v", os.Stderr, err)
		return
	}
}

type SDLWindow struct {
	*sdl.Window
	sync.RWMutex
	Device
	Position
	renderer      *sdl.Renderer
	texture       *sdl.Texture
	isCudaSupport bool
	once          sync.Once

	frameWidth  int
	frameHeight int
	scaleX      float32
	scaleY      float32
	font        *ttf.Font
}

func NewSDLWindow(pos Position, dev Device, isCuda bool) *SDLWindow {
	var err error
	var window *sdl.Window
	var renderer *sdl.Renderer
	sdl.Do(func() {
		window, err = sdl.CreateWindow(WindowTitle, int32(pos.x), int32(pos.y), int32(pos.width), int32(pos.height),
			sdl.WINDOW_RESIZABLE|sdl.WINDOW_ALWAYS_ON_TOP|sdl.WINDOW_BORDERLESS|sdl.WINDOW_VULKAN)
		if err != nil {
			log.Errorf("Failed to create window: %v , %v", os.Stderr, err)
			return
		}

		// Create renderer
		renderer, err = sdl.CreateRenderer(window, -1, sdl.RENDERER_ACCELERATED)
		if err != nil {
			log.Errorf("Failed to create renderer: %v,%v", os.Stderr, err)
			return
		}
	})

	// Create texture
	return &SDLWindow{
		Position:      pos,
		Device:        dev,
		Window:        window,
		renderer:      renderer,
		isCudaSupport: isCuda,
	}
}

func (s *SDLWindow) initWindow(width, height int) {
	var err error

	if s.isCudaSupport {
		s.texture, err = s.renderer.CreateTexture(uint32(C.SDL_PIXELFORMAT_NV12), sdl.TEXTUREACCESS_STREAMING, int32(width), int32(height))
	} else {
		s.texture, err = s.renderer.CreateTexture(sdl.PIXELFORMAT_IYUV, sdl.TEXTUREACCESS_STREAMING, int32(width), int32(height))
	}

	s.frameWidth = width
	s.frameHeight = height
	s.scaleX = float32(s.Position.width) / float32(s.frameWidth)
	s.scaleY = float32(s.Position.height) / float32(s.frameHeight)

	if err != nil {
		log.Errorf("Failed to create texture: %v, %v", os.Stderr, err)
		return
	}
	s.font, err = ttf.OpenFont("song.ttf", int(54.0*s.scaleX))
	if err != nil {
		log.Errorf("Failed to create texture: %v,%v", os.Stderr, err)
		return
	}
}

func (s *SDLWindow) Close() error {
	var err error
	sdl.Do(func() {
		s.font.Close()
		s.texture.Destroy()
		s.renderer.Destroy()
		err = s.Window.Destroy()
	})
	s.Window = nil
	return err
}

func (s *SDLWindow) IsOpen() bool {
	return s.Window != nil
}

func (s *SDLWindow) Hide() {
	sdl.Do(func() {
		s.Window.Hide()
		// s.Window.Minimize()
	})
}

func (s *SDLWindow) Show() {
	sdl.Do(func() {
		s.Window.Show()
		// s.Window.Restore()
		s.Window.SetAlwaysOnTop(true)
	})
}

func (s *SDLWindow) MoveWindow(x int, y int) {
	sdl.Do(func() {
		s.Window.SetPosition(int32(x), int32(y))
	})
	s.Position.x = x
	s.Position.y = y
}

func (s *SDLWindow) ResizeWindow(width int, height int) {
	sdl.Do(func() {
		s.Window.SetSize(int32(width), int32(height))
	})
	s.Position.width = width
	s.Position.height = height
}

func (s *SDLWindow) IMShow(frame *ffmpeg.VideoFrame, sei []*pb.PreviewInfo) {
	if frame != nil {
		defer frame.Free()
	}

	var err error

	sdl.Do(func() {
		if frame.NV12 != nil {
			nv12 := frame.NV12
			s.once.Do(func() {
				s.initWindow(nv12.Width, nv12.Height)
			})
			err = s.texture.UpdateNV(nil, nv12.YPlane, nv12.YPitch, nv12.UVPlane, nv12.UVPitch)
		} else if frame.YUV != nil {
			yuv := frame.YUV
			s.once.Do(func() {
				s.initWindow(yuv.Width, yuv.Height)
			})
			err = s.texture.UpdateYUV(nil, yuv.YPlane, yuv.YPitch, yuv.UPlane, yuv.UPitch, yuv.VPlane, yuv.VPitch)
		}
		if err != nil {
			log.Errorf("Failed to update texture: %v, %v", os.Stderr, err)
			return
		}

		// Clear renderer
		err = s.renderer.Clear()
		if err != nil {
			log.Errorf("Failed to clear renderer: %v, %v", os.Stderr, err)
			return
		}

		// Render texture
		err = s.renderer.Copy(s.texture, nil, nil)
		if err != nil {
			log.Errorf("Failed to copy texture: %v,%v", os.Stderr, err)
			return
		}

		s.drawOverlayImage(sei)

		// Present screen
		s.renderer.Present()
	})
}

func (s *SDLWindow) WaitKey(delay int) int {
	var err error
	sdl.Do(func() {
		for event := sdl.PollEvent(); event != nil; event = sdl.PollEvent() {
			switch event.(type) {
			case *sdl.QuitEvent:

			case *sdl.WindowEvent:

				// 窗口大小变化事件
				if event.(*sdl.WindowEvent).Event == sdl.WINDOWEVENT_SIZE_CHANGED {
					// 获取新的窗口大小
					width, height := s.Window.GetSize()
					s.Lock()
					defer s.Unlock()
					// 计算比例因子
					s.scaleX = float32(width) / float32(s.frameWidth)
					s.scaleY = float32(height) / float32(s.frameHeight)
					s.font.Close()
					s.font, err = ttf.OpenFont("song.ttf", int(54.0*s.scaleX))
					if err != nil {
						log.Errorf("Failed to copy texture: %v,%v", os.Stderr, err)
						return
					}
				}
			}
		}
	})
	// sdl.Delay(uint32(delay))
	return 0
}

func (s *SDLWindow) GetPosition() Position {
	return s.Position
}

func (s *SDLWindow) GetDevice() Device {
	return s.Device
}

func (cv *SDLWindow) GetType() string {
	return "sdl"
}

func NewWindow(pos Position, dev Device, useOpencv bool, isCUDA bool) Window {
	return NewSDLWindow(pos, dev, isCUDA)
}
