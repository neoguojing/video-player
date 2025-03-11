//go:build !sdl
// +build !sdl

package ffmpeg

/*
#include "ffmpeg.h"
*/
import "C"
import (
	"fmt"
	"image"
	"reflect"
	"sync"
	"time"
	"unsafe"

	log "github.com/sirupsen/logrus"

	"videoplayer/joy4/av"

	"videoplayer/joy4/codec/h264parser"

	"gocv.io/x/gocv"
)

type DecodeMode int

const (
	DecodeModeCPU = iota
	DecodeModeCuda
	DecodeModeQSV
)

func (d DecodeMode) String() string {
	switch d {
	case DecodeModeCPU:
		return "CPU"
	case DecodeModeCuda:
		return "CUDA"
	case DecodeModeQSV:
		return "QSV"
	default:
		return "Unknown"
	}
}

type VideoDecoder struct {
	ff        *ffctx
	Extradata []byte
	bgrBuffer *C.uint8_t
	sync.RWMutex

	Mode DecodeMode
}

func (self *VideoDecoder) Setup() (err error) {
	ff := &self.ff.ff

	if len(self.Extradata) > 0 {
		ff.codecCtx.extradata = (*C.uint8_t)(unsafe.Pointer(&self.Extradata[0]))
		ff.codecCtx.extradata_size = C.int(len(self.Extradata))
	}
	if C.avcodec_open2(ff.codecCtx, ff.codec, nil) != 0 {
		err = fmt.Errorf("ffmpeg: decoder: avcodec_open2 failed")
		return
	}
	return
}

func (self *VideoDecoder) Destroy() {
	self.Lock()
	defer self.Unlock()

	if self.ff != nil {
		freeFFCtx(self.ff)
		self.ff = nil
	}

	if self.bgrBuffer != nil {
		C.free(unsafe.Pointer(self.bgrBuffer))
		self.bgrBuffer = nil
	}
	self.Extradata = nil
}

func fromCPtr(buf unsafe.Pointer, size int) (ret []uint8) {
	hdr := (*reflect.SliceHeader)((unsafe.Pointer(&ret)))
	hdr.Cap = size
	hdr.Len = size
	hdr.Data = uintptr(buf)
	return
}

type VideoFrame struct {
	Mat       *gocv.Mat
	Image     image.Image
	frame     *C.AVFrame
	bgrBuffer *C.uint8_t
	NV12      *NV12
	YUV       *YUV
}

func (self *VideoFrame) Free() {
	if self.Mat != nil {
		self.Mat.Close()
		self.Mat = nil
	}

	if self.Image != nil {
		self.Image = nil
	}

	if self.NV12 != nil {
		self.NV12.YPlane = nil
		self.NV12.UVPlane = nil
		self.NV12 = nil
	}

	if self.YUV != nil {
		self.YUV.YPlane = nil
		self.YUV.UPlane = nil
		self.YUV.VPlane = nil
		self.YUV = nil
	}

	if self.frame != nil {
		C.av_frame_free(&self.frame)
		self.frame = nil
	}

	// if self.bgrBuffer != nil {
	// 	C.free(unsafe.Pointer(self.bgrBuffer))
	// }

}

func freeVideoFrame(self *VideoFrame) {
	self.Free()
}

func yuv4202Image(frame *C.AVFrame) *VideoFrame {
	w := int(frame.width)
	h := int(frame.height)
	ys := int(frame.linesize[0])
	cs := int(frame.linesize[1])

	// fmt.Println("yuv4202Image--------------------------", w, h, ys, cs, int(frame.linesize[2]))
	img := &VideoFrame{
		// Mat: mat,
		Image: &image.YCbCr{
			Y:              fromCPtr(unsafe.Pointer(frame.data[0]), ys*h),
			Cb:             fromCPtr(unsafe.Pointer(frame.data[1]), cs*h/2),
			Cr:             fromCPtr(unsafe.Pointer(frame.data[2]), cs*h/2),
			YStride:        ys,
			CStride:        cs,
			SubsampleRatio: image.YCbCrSubsampleRatio420,
			Rect:           image.Rect(0, 0, w, h),
		}, frame: frame}
	return img
}

type NV12 struct {
	Width   int
	Height  int
	YPlane  []byte
	YPitch  int
	UVPlane []byte
	UVPitch int
}

func nv12GoByte(frame *C.AVFrame) *VideoFrame {
	w := int(frame.width)
	h := int(frame.height)
	ys := int(frame.linesize[0])
	cs := int(frame.linesize[1])

	nv12 := &NV12{
		YPlane:  fromCPtr(unsafe.Pointer(frame.data[0]), ys*h),
		YPitch:  ys,
		UVPlane: fromCPtr(unsafe.Pointer(frame.data[1]), cs*h),
		UVPitch: cs,
		Width:   w,
		Height:  h,
	}

	img := &VideoFrame{
		NV12:  nv12,
		frame: frame,
	}
	return img

}

type YUV struct {
	Width  int
	Height int
	YPlane []byte
	YPitch int
	UPlane []byte
	UPitch int
	VPlane []byte
	VPitch int
}

func yuvGoByte(frame *C.AVFrame) *VideoFrame {
	w := int(frame.width)
	h := int(frame.height)
	ys := int(frame.linesize[0])
	cs := int(frame.linesize[1])

	yuv := &YUV{
		YPlane: fromCPtr(unsafe.Pointer(frame.data[0]), ys*h),
		YPitch: ys,
		UPlane: fromCPtr(unsafe.Pointer(frame.data[1]), cs*h/2),
		UPitch: cs,
		VPlane: fromCPtr(unsafe.Pointer(frame.data[2]), cs*h/2),
		VPitch: cs,
		Width:  w,
		Height: h,
	}

	img := &VideoFrame{
		YUV:   yuv,
		frame: frame,
	}
	return img

}

func (self *VideoDecoder) Decode(pkt []byte) (img *VideoFrame, err error) {
	ff := &self.ff.ff
	start := time.Now()
	cgotimg := C.int(0)
	var frame *C.AVFrame
	var cerr C.int

	cerr = C.wrap_avcodec_decode_video3(ff.codecCtx, &frame, unsafe.Pointer(&pkt[0]), C.int(len(pkt)), &cgotimg)

	if cerr < C.int(0) {
		err = fmt.Errorf("ffmpeg: wrap_avcodec_decode_video3 failed: %d,%d", int(cerr), cgotimg)
		return
	}
	log.Debug("XXXXXXXXXXXXXXXXXXXXXXwrap_avcodec_decode_video3", time.Since(start))
	if cgotimg != C.int(0) {
		img = yuv4202Image(frame)
	}
	log.Debug("XXXXXXXXXXXXXXXXXXXXXXYCbCr", time.Since(start))
	return
}

func (self *VideoDecoder) Decode1(pkt []byte, isKeyFrame bool) (img *VideoFrame, err error) {
	ff := &self.ff.ff

	cgotimg := C.int(0)
	// 在第一次使用时分配内存
	var frame *C.AVFrame
	// frame = C.av_frame_alloc()
	// defer C.av_frame_free(&frame)

	// if isKeyFrame {
	// 	C.avcodec_flush_buffers(ff.codecCtx)
	// }
	// cerr := C.wrap_avcodec_decode_video2(ff.codecCtx, frame, unsafe.Pointer(&pkt[0]), C.int(len(pkt)), &cgotimg)
	cerr := C.wrap_avcodec_decode_video3(ff.codecCtx, &frame, unsafe.Pointer(&pkt[0]), C.int(len(pkt)), &cgotimg)
	if cerr < C.int(0) {
		err = fmt.Errorf("ffmpeg: wrap_avcodec_decode_video3 failed: %d,%d,%d", int(cerr), cgotimg, len(pkt))
		return
	}

	if cgotimg != C.int(0) {
		// 调用C函数
		C.convertToBgr(&ff.swsContext, frame, &self.bgrBuffer)
		if self.bgrBuffer == nil {
			err = fmt.Errorf("ffmpeg: convertToBgr failed")
			return
		}

		width := int(frame.width)
		height := int(frame.height)

		var bgrSlice []byte
		bgrSlice = fromCPtrInByte(unsafe.Pointer(self.bgrBuffer), int(frame.linesize[0]*frame.height))

		log.Debug("bgrSlice", len(bgrSlice))
		var mat gocv.Mat
		mat, err = gocv.NewMatFromBytes(height, width, gocv.MatTypeCV8UC3, bgrSlice)
		if err != nil {
			err = fmt.Errorf("ffmpeg: NewMatFromBytes failed: %v", err)
			return
		}

		img = &VideoFrame{
			Mat:   &mat,
			frame: frame,
			// bgrBuffer: bgrBuffer,
		}
		// runtime.SetFinalizer(img, freeVideoFrame)

	}

	return
}

func (self *VideoDecoder) Decode2(pkt []byte, isKeyFrame bool) (img *VideoFrame, err error) {
	ff := &self.ff.ff

	// if isKeyFrame {
	// 	C.avcodec_flush_buffers(ff.codecCtx)
	// }

	// 在第一次使用时分配内存
	var frame *C.AVFrame
	frame = C.av_frame_alloc()
	defer C.av_frame_free(&frame)

	// cerr := C.wrap_avcodec_decode_video2(ff.codecCtx, frame, unsafe.Pointer(&pkt[0]), C.int(len(pkt)), &cgotimg)
	cerr := C.wrap_avcodec_decode_video4(&ff.swsContext, ff.codecCtx, frame, &self.bgrBuffer, unsafe.Pointer(&pkt[0]), C.int(len(pkt)))
	if cerr < C.int(0) {
		err = fmt.Errorf("ffmpeg: wrap_avcodec_decode_video3 failed: %d,%d,%d", int(cerr), len(pkt))
		return
	}

	width := int(frame.width)
	height := int(frame.height)

	var bgrSlice []byte
	bgrSlice = fromCPtrInByte(unsafe.Pointer(self.bgrBuffer), int(frame.linesize[0]*frame.height))

	log.Debug("bgrSlice", len(bgrSlice))
	var mat gocv.Mat
	// mat, err = gocv.NewMatFromBytes(height, width, gocv.MatTypeCV8UC3, bgrSlice)
	mat, err = gocv.NewMatFromBytes(height, width, gocv.MatTypeCV8UC4, bgrSlice)
	if err != nil {
		err = fmt.Errorf("ffmpeg: NewMatFromBytes failed: %v", err)
		return
	}

	img = &VideoFrame{
		Mat: &mat,
		// bgrBuffer: bgrBuffer,
	}
	// runtime.SetFinalizer(img, freeVideoFrame)

	return
}

func fromCPtrInByte(buf unsafe.Pointer, size int) (ret []byte) {
	hdr := (*reflect.SliceHeader)((unsafe.Pointer(&ret)))
	hdr.Cap = size
	hdr.Len = size
	hdr.Data = uintptr(buf)
	return
}

func avFrameToMat(frame *C.AVFrame) *gocv.Mat {

	w := int(frame.width)
	h := int(frame.height)
	ys := int(frame.linesize[0])
	cs := int(frame.linesize[1])

	Y := fromCPtrInByte(unsafe.Pointer(frame.data[0]), ys*h)
	Cb := fromCPtrInByte(unsafe.Pointer(frame.data[1]), cs*h/2)
	Cr := fromCPtrInByte(unsafe.Pointer(frame.data[2]), cs*h/2)
	data := make([]byte, 0)
	data = append(data, Y...)
	data = append(data, Cb...)
	data = append(data, Cr...)
	mat, _ := gocv.NewMatFromBytes(w, h, gocv.MatTypeCV8UC1, data)
	return &mat
}

func NewVideoDecoder(stream av.CodecData) (dec *VideoDecoder, err error) {
	_dec := &VideoDecoder{
		Mode: DecodeModeCPU,
	}
	var id uint32
	var mode DecodeMode

	switch stream.Type() {
	case av.H264:
		h264 := stream.(h264parser.CodecData)
		_dec.Extradata = h264.AVCDecoderConfRecordBytes()
		id = C.AV_CODEC_ID_H264

	default:
		err = fmt.Errorf("ffmpeg: NewVideoDecoder codec=%v unsupported", stream.Type())
		return
	}

	c := C.avcodec_find_decoder(id)
	if c == nil || C.avcodec_get_type(id) != C.AVMEDIA_TYPE_VIDEO {
		err = fmt.Errorf("ffmpeg: cannot find video decoder codecId=%d", id)
		return
	}

	if _dec.ff, mode, err = newFFCtxByCodec(c); err != nil {
		return
	}
	mode = mode

	if err = _dec.Setup(); err != nil {
		return
	}

	dec = _dec
	return
}
