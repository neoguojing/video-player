//go:build sdl
// +build sdl

package player

// #include <SDL2/SDL_pixels.h>
import "C"
import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"os"
	"time"
	"videoplayer/pb"

	log "github.com/sirupsen/logrus"
	"github.com/veandco/go-sdl2/sdl"
	"github.com/veandco/go-sdl2/ttf"
)

const (
	WindowTitle  = "SDL Video Player"
	WindowWidth  = 1920
	WindowHeight = 1080
)

var (
	font *ttf.Font
)

func init() {
	var err error
	if err = ttf.Init(); err != nil {
		log.Fatal("load font failed")
		return
	}

	// if font, err = ttf.OpenFont("song.ttf", 54); err != nil {
	// 	log.Fatal("load font failed")
	// }
}

func (s *SDLWindow) drawOverlayImage(objectInfos []*pb.PreviewInfo) error {

	if len(objectInfos) == 0 {
		log.Warn("getOverlayImage objectInfos was invalid!!!")
		return errors.New("getOverlayImage objectInfos was invalid")
	}
	var err error

	rectsByColor := make(map[uint32][]sdl.Rect)
	points := make([]image.Point, 0)
	texts := make([]string, 0)

	s.RLock()
	for _, previewInfo := range objectInfos {
		if previewInfo.Objects == nil {
			continue
		}

		for _, obj := range previewInfo.Objects {
			if obj.Bounding == nil || obj.Bounding.Vertices == nil || len(obj.Bounding.Vertices) != 2 {
				log.Debug("getOverlayImage objectInfos.Objects was invalid!!!")
				continue
			}

			if obj.ObjectType != pb.ObjectType_OBJECT_FACE {
				continue
			}

			log.Debug(obj.ObjectType, obj.Attributes)
			x1 := int(float32(obj.Bounding.Vertices[0].X) * s.scaleX)
			y1 := int(float32(obj.Bounding.Vertices[0].Y) * s.scaleY)
			x2 := int(float32(obj.Bounding.Vertices[1].X) * s.scaleX)
			y2 := int(float32(obj.Bounding.Vertices[1].Y) * s.scaleY)

			rect := image.Rect(x1, y1, x2, y2)
			margin := int(100.0 * s.scaleX)
			expandedRect := rect.Inset(-margin)

			var objColor = sdl.Color{R: 255, G: 0, B: 0, A: 0}

			if obj.Attributes != nil {
				if ifd, ok := obj.Attributes["ifd_extra_info"]; ok {
					extraInfo := map[string]string{}
					err := json.Unmarshal([]byte(ifd), &extraInfo)
					if err == nil {
						if text, ok1 := extraInfo["name"]; ok1 {
							objColor = sdl.Color{R: 0, G: 255, B: 0, A: 0}

							texts = append(texts, text)
							points = append(points, expandedRect.Min)
						}
					}
				}
			}

			if log.GetLevel() == log.DebugLevel {
				texts = append(texts, fmt.Sprintf("origin-size:%vx%v",
					obj.Bounding.Vertices[1].X-obj.Bounding.Vertices[0].X,
					obj.Bounding.Vertices[1].Y-obj.Bounding.Vertices[0].Y))
				points = append(points, expandedRect.Min)
			}

			if _, ok := rectsByColor[objColor.Uint32()]; !ok {
				rectsByColor[objColor.Uint32()] = make([]sdl.Rect, 0)
			}

			rectsByColor[objColor.Uint32()] = append(rectsByColor[objColor.Uint32()], sdl.Rect{
				X: int32(expandedRect.Min.X),
				Y: int32(expandedRect.Min.Y),
				W: int32(expandedRect.Dx()),
				H: int32(expandedRect.Dy()),
			})
		}
	}
	s.RUnlock()

	if len(rectsByColor) > 0 {
		start := time.Now()
		// wg := sync.WaitGroup{}
		s.RLock()
		for i, t := range texts {
			// wg.Add(1)
			// go func(idx int, text string) {
			// 	defer wg.Done()
			s.drawText(t, points[i])
			// }(i, t)
		}
		s.RUnlock()

		log.Debug("draw texts cost:", time.Since(start))
		for c, rects := range rectsByColor {
			// wg.Add(1)
			// go func(c uint32, rects []sdl.Rect) {
			// 	defer wg.Done()
			s.drawRect(rects, GetColorFromUint32(c))
			// }(c, rects)
		}
		log.Debug("draw rects cost:", time.Since(start))
		// wg.Wait()
	}

	return err
}

func (s *SDLWindow) drawText(input string, point image.Point) {
	var err error
	// Create a red text with the font
	var textSurface *sdl.Surface
	if textSurface, err = s.font.RenderUTF8Blended(input, sdl.Color{R: 0, G: 255, B: 0, A: 0}); err != nil {
		return
	}
	defer textSurface.Free()

	textTexture, err := s.renderer.CreateTextureFromSurface(textSurface)
	if err != nil {
		log.Errorf("Failed to create texture from surface: %v, %v", os.Stderr, err)
		return
	}
	defer textTexture.Destroy()

	textRect := &sdl.Rect{X: int32(point.X), Y: int32(point.Y), W: textSurface.W, H: textSurface.H}
	s.renderer.Copy(textTexture, nil, textRect)

}

func (s *SDLWindow) drawRect(rects []sdl.Rect, color sdl.Color) {
	s.renderer.SetDrawColor(color.R, color.G, color.B, color.A)
	s.renderer.DrawRects(rects)
}

func GetColorFromUint32(v uint32) sdl.Color {
	r := uint8((v >> 24) & 0xFF)
	g := uint8((v >> 16) & 0xFF)
	b := uint8((v >> 8) & 0xFF)
	a := uint8(v & 0xFF)
	color := sdl.Color{R: r, G: g, B: b, A: a}
	return color
}
