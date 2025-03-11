package text2image

import (
	"image"
	"image/color"
	"testing"

	"github.com/fogleman/gg"
)

func TestDrawRectangle(t *testing.T) {
	// 创建一个测试用的图像
	img, _ := readJPEG("test.jpeg")

	// 创建一个颜色
	c := color.RGBA{255, 0, 0, 255}

	// 调用函数进行绘制
	_, err := DrawRectangle(img, []image.Rectangle{image.Rect(100, 100, 500, 500)}, []color.Color{c})
	if err != nil {
		t.Errorf("Error drawing rectangle: %v", err)
	}

	// saveJPEG(resultImg, "rec.jpg")
}

func TestDrawText(t *testing.T) {
	// 创建一个测试用的图像
	img, _ := readJPEG("test.jpeg")

	// 创建一个颜色
	c := color.RGBA{255, 0, 0, 255}
	rect := image.Rect(100, 100, 50, 50)
	// 调用函数进行绘制
	_, err := DrawChineseTextOnImage(img, []image.Point{rect.Min}, []string{"中国"}, []color.Color{c})
	if err != nil {
		t.Errorf("Error drawing rectangle: %v", err)
	}

	// saveJPEG(resultImg, "rec.jpg")
}

func TestDrawRectangCircle(t *testing.T) {
	// 创建一个测试用的图像
	img, _ := readJPEG("test.jpeg")

	// 创建一个颜色
	color := color.RGBA{255, 0, 0, 255}

	// 调用函数进行绘制
	_, err := DrawCircle(img, image.Rect(100, 100, 500, 500), color)
	if err != nil {
		t.Errorf("Error drawing rectangle: %v", err)
	}

	// saveJPEG(resultImg, "rec.jpg")
}

func TestDrawRectAndText(t *testing.T) {
	// 创建一个测试用的图像
	img, _ := readJPEG("test.jpeg")

	// 创建一个颜色
	c := color.RGBA{255, 0, 0, 255}

	dc := gg.NewContextForImage(img)
	// 调用函数进行绘制
	rect := image.Rect(100, 100, 500, 500)
	_, err := DrawRectAndText(dc, []image.Point{rect.Min}, []string{"中国"}, []color.Color{c}, []image.Rectangle{rect}, []color.Color{c})
	if err != nil {
		t.Errorf("Error drawing rectangle: %v", err)
	}

	// saveJPEG(resultImg, "rec.jpg")
}
