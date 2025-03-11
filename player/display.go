package player

import (
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"time"
	"videoplayer/ffmpeg"
	"videoplayer/pb"
	"videoplayer/util/text2image"

	"github.com/fogleman/gg"
	log "github.com/sirupsen/logrus"
	"gocv.io/x/gocv"
)

type ParsedData struct {
	Frame        []byte
	PreviewInfos []*pb.PreviewInfo
	StartTime    time.Time
	IsKeyFrame   bool
	NaluType     int
	Pts          int64
	Mat          *gocv.Mat
}

func (d *Demuxer) Decode(pkt []byte, pts int64, startTime time.Time) (*ffmpeg.VideoFrame, error) {

	decodeFrame, err := d.decoder.Decode(pkt)
	if err != nil {
		return nil, err
	}
	if decodeFrame == nil {
		return nil, errors.New("decode result was nil")
	}
	decodeCost := time.Since(startTime)
	log.Debug("decodeCost:", decodeCost)
	// defer decodeFrame.Free()
	if decodeFrame.Mat != nil {
		if len(d.lastPreviewInfo) > 0 {
			diff := pts/1e6 - d.lastPreviewInfo[0].Timestamp/1e6
			log.Debug("-----------------------", pts/1e6, d.lastPreviewInfo[0].Timestamp/1e6, diff)
			// if math.Abs(float64(data.Pts-lastPreviewInfo[0].Timestamp))/1e6 < 100 {
			getOverlayImage(d.lastPreviewInfo, &decodeFrame.Mat)
			// }
			drawCost := time.Since(startTime)
			log.Debug("drawCost:**********************", drawCost)
		}
		return decodeFrame, nil
	} else if decodeFrame.Image != nil {
		if len(d.lastPreviewInfo) > 0 {
			diff := pts/1e6 - d.lastPreviewInfo[0].Timestamp/1e6
			log.Debug("-----------------------", pts/1e6, d.lastPreviewInfo[0].Timestamp/1e6, diff)
			// if math.Abs(float64(data.Pts-lastPreviewInfo[0].Timestamp))/1e6 < 100 {
			tmpImage, err := getOverlayImageOnImage(d.lastPreviewInfo, decodeFrame.Image)
			if err == nil && tmpImage != nil {
				decodeFrame.Image = tmpImage
			}
			// }
			drawCost := time.Since(startTime)
			log.Debug("drawCost:**********************", drawCost)
		}
		mat, err := gocv.ImageToMatRGBA(decodeFrame.Image)
		if err != nil {
			log.Debug("ImageToMatRGBA  failed:", err)
			return nil, err
		}
		decodeFrame.Mat = &mat
		return decodeFrame, nil
	} else if decodeFrame.NV12 != nil {
		return decodeFrame, nil
	} else if decodeFrame.YUV != nil {
		return decodeFrame, nil
	}

	return nil, errors.New("decode result was empty")
}

func getOverlayImage(objectInfos []*pb.PreviewInfo, frame **gocv.Mat) {
	if len(objectInfos) == 0 {
		log.Debug("getOverlayImage objectInfos was invalid!!!")
		return
	}

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
			x1 := int(obj.Bounding.Vertices[0].X)
			y1 := int(obj.Bounding.Vertices[0].Y)
			x2 := int(obj.Bounding.Vertices[1].X)
			y2 := int(obj.Bounding.Vertices[1].Y)

			rect := image.Rect(x1, y1, x2, y2)
			margin := 100
			expandedRect := rect.Inset(-margin)

			var objColor = color.RGBA{255, 0, 0, 0}

			if obj.Attributes != nil {
				if ifd, ok := obj.Attributes["ifd_extra_info"]; ok {
					extraInfo := map[string]string{}
					err := json.Unmarshal([]byte(ifd), &extraInfo)
					if err == nil {
						if text, ok1 := extraInfo["name"]; ok1 {
							objColor = color.RGBA{0, 255, 0, 0}
							// getOverlayText(text, frame, expandedRect.Min, objColor)
							newMat, err := text2image.DrawChineseText(*frame, expandedRect.Min, text, objColor)
							if err == nil && newMat != nil {
								(*frame).Close()
								*frame = newMat
							}

						}
					}
				}
			}

			// 在视频帧上绘制矩形框
			gocv.Rectangle(*frame, expandedRect, objColor, 2)
		}
	}

}

func getOverlayImageOnImage(objectInfos []*pb.PreviewInfo, frame image.Image) (image.Image, error) {
	if len(objectInfos) == 0 {
		log.Debug("getOverlayImage objectInfos was invalid!!!")
		return nil, errors.New("getOverlayImage objectInfos was invalid")
	}
	var err error
	var img image.Image

	colors := make([]color.Color, 0)
	rects := make([]image.Rectangle, 0)
	points := make([]image.Point, 0)
	texts := make([]string, 0)
	tcolors := make([]color.Color, 0)
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
			x1 := int(obj.Bounding.Vertices[0].X)
			y1 := int(obj.Bounding.Vertices[0].Y)
			x2 := int(obj.Bounding.Vertices[1].X)
			y2 := int(obj.Bounding.Vertices[1].Y)

			rect := image.Rect(x1, y1, x2, y2)
			margin := 100
			expandedRect := rect.Inset(-margin)

			var objColor = color.RGBA{255, 0, 0, 0}

			if obj.Attributes != nil {
				if ifd, ok := obj.Attributes["ifd_extra_info"]; ok {
					extraInfo := map[string]string{}
					err := json.Unmarshal([]byte(ifd), &extraInfo)
					if err == nil {
						if text, ok1 := extraInfo["name"]; ok1 {
							objColor = color.RGBA{0, 255, 0, 0}

							tcolors = append(tcolors, objColor)
							texts = append(texts, text)
							points = append(points, expandedRect.Min)
						}
					}
				}
			}

			colors = append(colors, objColor)
			rects = append(rects, expandedRect)
		}
	}

	dc := gg.NewContextForImage(frame)
	img, err = text2image.DrawRectAndText(dc, points, texts, tcolors, rects, colors)
	if err != nil {
		log.Debug(err)
	}
	return img, err
}

func getOverlayText(text string, frame *gocv.Mat, position image.Point, color color.RGBA) {
	fontFace := gocv.FontHersheyPlain
	fontScale := 1.2
	thickness := 2
	gocv.PutText(frame, text, position, fontFace, fontScale, color, thickness)
}
