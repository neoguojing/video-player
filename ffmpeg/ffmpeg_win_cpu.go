//go:build windows && cpu
// +build windows,cpu

package ffmpeg

/*
#cgo windows CFLAGS: -I${SRCDIR}/../libs/windows/ffmpeg_cuda/include/ -I${SRCDIR}/../libs/windows/mfx/
#cgo windows LDFLAGS: -L${SRCDIR}/../libs/windows/ffmpeg_cuda/bin/ -lavformat -lavutil -lavcodec -lswscale
#include <windows.h>
#include "ffmpeg.h"
*/
import "C"

import (
	"unsafe"
)

const (
	QUIET   = int(C.AV_LOG_QUIET)
	PANIC   = int(C.AV_LOG_PANIC)
	FATAL   = int(C.AV_LOG_FATAL)
	ERROR   = int(C.AV_LOG_ERROR)
	WARNING = int(C.AV_LOG_WARNING)
	INFO    = int(C.AV_LOG_INFO)
	VERBOSE = int(C.AV_LOG_VERBOSE)
	DEBUG   = int(C.AV_LOG_DEBUG)
	TRACE   = int(C.AV_LOG_TRACE)
)

func HasEncoder(name string) bool {
	return C.avcodec_find_encoder_by_name(C.CString(name)) != nil
}

func HasDecoder(name string) bool {
	return C.avcodec_find_decoder_by_name(C.CString(name)) != nil
}

//func EncodersList() []string
//func DecodersList() []string

func SetLogLevel(level int) {
	C.av_log_set_level(C.int(level))
}

type ffctx struct {
	ff C.FFCtx
}

func newFFCtxByCodec(codec *C.AVCodec) (ff *ffctx, mode DecodeMode, err error) {
	ff = &ffctx{}

	mode = DecodeModeCPU

	ff.ff.codec = codec
	ff.ff.codecCtx = C.avcodec_alloc_context3(codec)
	ff.ff.codecCtx.thread_count = 2
	ff.ff.profile = C.FF_PROFILE_UNKNOWN

	// runtime.SetFinalizer(ff, freeFFCtx)
	return
}

func freeFFCtx(self *ffctx) {
	ff := &self.ff
	if ff.frame != nil {
		C.av_frame_free(&ff.frame)
	}
	if ff.codecCtx != nil {
		C.avcodec_close(ff.codecCtx)
		C.av_free(unsafe.Pointer(ff.codecCtx))
		ff.codecCtx = nil
	}
	if ff.options != nil {
		C.av_dict_free(&ff.options)
	}

	if ff.swsContext != nil {
		C.sws_freeContext(ff.swsContext)
	}

	if ff.hw_device_ctx != nil {
		C.av_buffer_unref(&ff.hw_device_ctx)
	}

}
