/*
 * Copyright (c) 2017 Jun Zhao
 * Copyright (c) 2017 Kaixuan Liu
 *
 * HW Acceleration API (video decoding) decode sample
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL
 * THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
 * THE SOFTWARE.
 */
 
/**
 * @file HW-accelerated decoding API usage.example
 * @example hw_decode.c
 *
 * Perform HW-accelerated decoding with output frames from HW video
 * surfaces.
 */
 
#include <stdio.h>
#include <libavcodec/avcodec.h>
#include <libavformat/avformat.h>
#include <libavutil/pixdesc.h>
#include <libavutil/hwcontext.h>
#include <libavutil/opt.h>
#include <libavutil/avassert.h>
#include <libavutil/imgutils.h>
#ifdef _WIN32
    #include <libavutil/hwcontext_qsv.h>
    #include <libavcodec/codec.h>
#endif

 typedef struct DecodeContext {
     AVBufferRef *hw_device_ref;
 } DecodeContext;

static enum AVPixelFormat hw_pix_fmt = AV_PIX_FMT_CUDA;

static enum AVPixelFormat get_hw_format(AVCodecContext *ctx,
                                        const enum AVPixelFormat *pix_fmts)
{
    const enum AVPixelFormat *p;

    for (p = pix_fmts; *p != -1; p++) {
        if (*p == hw_pix_fmt)
            return *p;
    }

    printf("Failed to get HW surface format.\n");
    return AV_PIX_FMT_NONE;
}

static int get_qsv_format(AVCodecContext *avctx, const enum AVPixelFormat *pix_fmts)
{
    while (*pix_fmts != AV_PIX_FMT_NONE) {
        if (*pix_fmts == AV_PIX_FMT_QSV) {
            return AV_PIX_FMT_QSV;
        }

        pix_fmts++;
    }

    fprintf(stderr, "The QSV pixel format not offered in get_format()\n");

    return AV_PIX_FMT_NONE;
}

#ifdef _WIN32
static enum AVPixelFormat get_hw_pix_format(const AVCodec *decoder,
                                        const enum AVHWDeviceType type)
{   
    int i = 0;
    for (i = 0;; i++) {
        const AVCodecHWConfig *config = avcodec_get_hw_config(decoder, i);
        if (!config) {
            printf("Decoder %s does not support device type %s.\n",
                    decoder->name, av_hwdevice_get_type_name(type));
            return AV_PIX_FMT_NONE;
        }
        if (config->methods & AV_CODEC_HW_CONFIG_METHOD_HW_DEVICE_CTX &&
            config->device_type == type) {
            hw_pix_fmt = config->pix_fmt;
            return config->pix_fmt;
        }
    }

    return AV_PIX_FMT_NONE;
}

static int hw_decoder_init(AVBufferRef **hw_device_ctx,AVCodecContext *ctx,const AVCodec *decoder, const enum AVHWDeviceType type)
{
    int err = 0;
    char errstr[128];
    
    get_hw_pix_format(decoder,type);
    
    if ((err = av_hwdevice_ctx_create(hw_device_ctx, type,
                                      NULL, NULL, 0)) < 0) {
        av_strerror(err, errstr, sizeof(errstr));
		printf("cuda av_hwdevice_ctx_create error %d %s\n",err,errstr);
        return err;
    }

    ctx->get_format  = get_hw_format;
    ctx->hw_device_ctx = av_buffer_ref(*hw_device_ctx);
 
    return err;
}

static int frame_gpu_to_cpu(AVFrame **frame)
{   
    int ret = 0;
    char errstr[128];

    if ((*frame)->format == AV_PIX_FMT_CUDA || (*frame)->format == AV_PIX_FMT_QSV) {
        AVFrame *sw_frame = av_frame_alloc();
        /* retrieve data from GPU to CPU */
        if ((ret = av_hwframe_transfer_data(sw_frame, *frame, 0)) < 0) {
            av_strerror(ret, errstr, sizeof(errstr));
            printf("Error transferring the data to system memory %d %s\n",ret,errstr);
            av_frame_free(&sw_frame);
            return ret;
        }
        av_frame_free(frame);
        *frame = sw_frame;
    }

    return ret;
}
#else
static enum AVPixelFormat get_hw_pix_format(const AVCodec *decoder,
                                        const enum AVHWDeviceType type)
{  
    return AV_PIX_FMT_YUV420P;
}
static int hw_decoder_init(AVBufferRef **hw_device_ctx,AVCodecContext *ctx,const AVCodec *decoder, const enum AVHWDeviceType type)
{
    return -1;
}

static int frame_gpu_to_cpu(AVFrame **frame){
    return 0;
}

#endif

static inline int qsv_codec_init(AVBufferRef **hwDeviceCtx) {
	int ret = 0;
	char errstr[128];

	ret = av_hwdevice_ctx_create(hwDeviceCtx, AV_HWDEVICE_TYPE_QSV,
                                 "auto", NULL, 0);
	if (ret < 0) {
		av_strerror(ret, errstr, sizeof(errstr));
		printf("qsv av_hwdevice_ctx_create error %d %s\n",ret,errstr);
		return ret;
	}

	return ret;
}

static inline const AVCodec* qsc_codec_finder() {

	/* initialize the decoder */
    AVCodec *decoder;
    decoder = avcodec_find_decoder_by_name("h264_qsv");
    if (!decoder) {
        fprintf(stderr, "The QSV decoder is not present in libavcodec\n");
        return NULL;
    }
    return decoder;
}

static inline void qsv_codec_setup(AVCodecContext *codecCtx,AVBufferRef *hwDeviceCtx) {

	// 将QSV硬件上下文与解码器关联
	codecCtx->hw_device_ctx = av_buffer_ref(hwDeviceCtx);
	codecCtx->get_format = get_qsv_format;
	return;
}