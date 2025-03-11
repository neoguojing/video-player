package text2image

import (
	log "github.com/sirupsen/logrus"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"os"
	"sync"

	"github.com/fogleman/gg"
	"github.com/golang/freetype"
	"gocv.io/x/gocv"
	"golang.org/x/image/font"
)

func readJPEG(filename string) (*image.RGBA, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}

	// 将图像转换为RGBA格式，以便进行绘制操作
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, image.Point{}, draw.Over)

	return rgba, nil
}

func getDrawImage(mat *gocv.Mat) (*image.RGBA, error) {
	img, err := mat.ToImage()
	if err != nil {
		return nil, err
	}

	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, image.Point{}, draw.Over)

	return rgba, nil
}

var freetypeContext *freetype.Context
var fontFace font.Face

func init() {
	// 设置字体
	var err error
	fontFace, err = gg.LoadFontFace("song.ttf", 54)
	if err != nil {
		log.Debug("Error loading font face:", err)
		return
	}
}
func DrawChineseText(mat *gocv.Mat, position image.Point, text string, color color.Color) (*gocv.Mat, error) {
	// img, _ := readJPEG("aaa.jpg")
	img, err := mat.ToImage()
	if err != nil {
		log.Debug(err)
		return nil, err
	}
	// 创建一个 gg.Context，它允许我们在图像上绘制
	dc := gg.NewContextForImage(img)
	dc.SetFontFace(fontFace)
	r, g, b, _ := color.RGBA()
	dc.SetRGB(float64(r)/65535.0, float64(g)/65535.0, float64(b)/65535.0)
	dc.DrawString(text, float64(position.X), float64(position.Y))
	tmp, err := gocv.ImageToMatRGBA(dc.Image())
	if err != nil {
		log.Debug(err)
		return nil, err
	}
	log.Debug("@@@@@@@@@@@@@@@@@@@@@@", mat.Ptr(), tmp.Ptr())
	return &tmp, nil
}

func DrawRectAndText(dc *gg.Context, positions []image.Point, texts []string, tcolors []color.Color,
	rects []image.Rectangle, colors []color.Color) (image.Image, error) {
	var wg sync.WaitGroup

	// 并发绘制矩形
	wg.Add(1)
	go func() {
		defer wg.Done()
		dc.SetLineWidth(10)
		for i, rect := range rects {
			x, y, w, h := float64(rect.Min.X), float64(rect.Min.Y), float64(rect.Dx()), float64(rect.Dy())
			r, g, b, _ := colors[i].RGBA()
			dc.SetRGB(float64(r)/65535.0, float64(g)/65535.0, float64(b)/65535.0)
			dc.DrawRectangle(x, y, w, h)
			dc.Stroke()
		}

	}()

	// 创建一个新的子路径
	// wg.Add(1)
	// go func() {
	// 	defer wg.Done()
	// 	dc.SetLineWidth(10)
	// 	dc.NewSubPath()

	// 	for i, rect := range rects {
	// 		x, y, w, h := float64(rect.Min.X), float64(rect.Min.Y), float64(rect.Dx()), float64(rect.Dy())
	// 		r, g, b, _ := colors[i].RGBA()
	// 		dc.SetRGB(float64(r)/65535.0, float64(g)/65535.0, float64(b)/65535.0)
	// 		// 定义路径的起点和线段
	// 		if i == 0 {
	// 			dc.MoveTo(x, y)
	// 		} else {
	// 			dc.LineTo(x, y)
	// 		}
	// 		dc.LineTo(x+w, y)
	// 		dc.LineTo(x+w, y+h)
	// 		dc.LineTo(x, y+h)
	// 	}

	// 	// 绘制路径轮廓
	// 	dc.Stroke()
	// }()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if len(texts) > 0 {
			dc.SetFontFace(fontFace)
			for i, text := range texts {
				r, g, b, _ := tcolors[i].RGBA()
				dc.SetRGB(float64(r)/65535.0, float64(g)/65535.0, float64(b)/65535.0)
				dc.DrawString(text, float64(positions[i].X), float64(positions[i].Y))
			}
		}
	}()

	// 等待所有绘制任务完成
	wg.Wait()
	// dc.SavePNG("rectandtext.png")
	return dc.Image(), nil
}

func DrawChineseTextOnImage(img image.Image, positions []image.Point, texts []string, colors []color.Color) (image.Image, error) {

	// 创建一个 gg.Context，它允许我们在图像上绘制
	dc := gg.NewContextForImage(img)
	dc.SetFontFace(fontFace)
	for i, text := range texts {
		r, g, b, _ := colors[i].RGBA()
		dc.SetRGB(float64(r)/65535.0, float64(g)/65535.0, float64(b)/65535.0)
		dc.DrawString(text, float64(positions[i].X), float64(positions[i].Y))
	}
	// dc.SavePNG("text.png")
	return dc.Image(), nil
}

func DrawRectangle(img image.Image, rects []image.Rectangle, colors []color.Color) (image.Image, error) {

	dc := gg.NewContextForImage(img)

	for i, rect := range rects {
		x, y, w, h := float64(rect.Min.X), float64(rect.Min.Y), float64(rect.Dx()), float64(rect.Dy())
		r, g, b, _ := colors[i].RGBA()
		dc.SetRGB(float64(r)/65535.0, float64(g)/65535.0, float64(b)/65535.0)
		dc.DrawRectangle(x, y, w, h)
		dc.Stroke()
	}

	// dc.SavePNG("rec.png")
	return dc.Image(), nil
}

func DrawCircle(img image.Image, rect image.Rectangle, color color.Color) (image.Image, error) {
	dc := gg.NewContextForImage(img)
	dc.DrawCircle(500, 500, 400)
	dc.SetRGB(0, 0, 0)
	dc.Fill()
	// dc.SavePNG("circle.png")
	return dc.Image(), nil
}

func createImage(width, height int, bgColor color.Color) *image.RGBA {
	rect := image.Rect(0, 0, width, height)
	img := image.NewRGBA(rect)
	draw.Draw(img, img.Bounds(), &image.Uniform{bgColor}, image.Point{}, draw.Src)
	return img
}

func saveJPEG(img image.Image, filename string) {
	file, err := os.Create(filename)
	if err != nil {
		log.Debug("Error creating output file:", err)
		return
	}
	defer file.Close()

	err = jpeg.Encode(file, img, nil)
	if err != nil {
		log.Debug("Error encoding image:", err)
		return
	}

	log.Debug("Image saved to", filename)
}
