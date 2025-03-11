#include <libavformat/avformat.h>
#include <libavcodec/avcodec.h>
#include <libavutil/avutil.h>
#include <libavutil/opt.h>
#include <libswscale/swscale.h>
#include <libavutil/imgutils.h>
#include <libavutil/hwcontext.h>
#include <string.h>
#include "hw.h"

#ifdef _WIN32
    #include <windows.h>
    #define MY_ALLOC(size) HeapAlloc(GetProcessHeap(), 0, size)
    #define MY_FREE(ptr) HeapFree(GetProcessHeap(), 0, ptr)
#else
    #define MY_ALLOC(size) malloc(size)
    #define MY_FREE(ptr) free(ptr)
#endif

typedef struct {
	AVCodec *codec;
	AVCodecContext *codecCtx;
	AVFrame *frame;
	AVDictionary *options;
	struct SwsContext* swsContext;
	int profile;

	enum AVPixelFormat hw_pix_fmt;
	enum AVHWDeviceType type;
	AVBufferRef *hw_device_ctx;
} FFCtx;

static inline int avcodec_profile_name_to_int(AVCodec *codec, const char *name) {
	const AVProfile *p;
	for (p = codec->profiles; p != NULL && p->profile != FF_PROFILE_UNKNOWN; p++)
		if (!strcasecmp(p->name, name))
			return p->profile;
	return FF_PROFILE_UNKNOWN;
}

static int wrap_avcodec_decode_video3(AVCodecContext *ctx, AVFrame **frame, void *data, int size, int *got) {
	struct AVPacket pkt = {.data = data, .size = size};
	char errstr[128];
	// 发送待解码的视频数据包
	int ret = avcodec_send_packet(ctx, &pkt);
	if (ret < 0) {
		av_strerror(ret, errstr, sizeof(errstr));
		printf("send_packet error %d %s\n",ret,errstr);
		return ret;
	}

	// 接收解码后的视频帧
	while (1) {
		if (!(*frame = av_frame_alloc())){
            printf("Can not alloc frame\n");
            return 2;
        }

		ret = avcodec_receive_frame(ctx, *frame);
		if (ret == AVERROR(EAGAIN) || ret == AVERROR_EOF) {
			av_strerror(ret, errstr, sizeof(errstr));
			printf("avcodec_receive_frame EOF %d %s\n",ret,errstr);
			av_frame_free(frame);
			return 1;
		} else if (ret < 0) {
			av_strerror(ret, errstr, sizeof(errstr));
			printf("Error during decoding %d %s\n",ret,errstr);
			av_frame_free(frame);
			return ret;
		} else {
			*got = 1;
			break;
		}
	}

	return 0;
}

static int wrap_avcodec_decode_video_with_hw(AVCodecContext *ctx, AVFrame **frame, void *data, int size, int *got) {
	struct AVPacket pkt = {.data = data, .size = size};
	char errstr[128];
	// 发送待解码的视频数据包
	int ret = avcodec_send_packet(ctx, &pkt);
	if (ret < 0) {
		av_strerror(ret, errstr, sizeof(errstr));
		printf("send_packet error %d %s\n",ret,errstr);
		return ret;
	}

	// 接收解码后的视频帧
	while (1) {
		if (!(*frame = av_frame_alloc())){
            printf("Can not alloc frame\n");
            return 2;
        }

		ret = avcodec_receive_frame(ctx, *frame);
		if (ret == AVERROR(EAGAIN) || ret == AVERROR_EOF) {
			av_strerror(ret, errstr, sizeof(errstr));
			printf("avcodec_receive_frame EOF %d %s\n",ret,errstr);
			av_frame_free(frame);
			return 1;
		} else if (ret < 0) {
			av_strerror(ret, errstr, sizeof(errstr));
			printf("Error during decoding %d %s\n",ret,errstr);
			av_frame_free(frame);
			return ret;
		} else {
			ret = frame_gpu_to_cpu(frame);
			if (ret < 0) {
				return ret;
			}
			*got = 1;
			break;
		}
	}

	return 0;
}


static void convertToBgr(struct SwsContext** swsContext,const struct AVFrame *frame,uint8_t** p_global_bgr_buffer) {
		if (*swsContext == NULL) {
			*swsContext = sws_getContext(frame->width, frame->height, AV_PIX_FMT_YUV420P,
				frame->width, frame->height, AV_PIX_FMT_BGR24,0, 0, 0, 0);
			printf("--------------------------------------------------");
		}

		int linesize[8] = {frame->linesize[0] * 3};
		int num_bytes = av_image_get_buffer_size(AV_PIX_FMT_BGR24, frame->width, frame->height, 1);

		if (!*p_global_bgr_buffer) {
			*p_global_bgr_buffer = (uint8_t*)MY_ALLOC(num_bytes * sizeof(uint8_t));
		}
		// *p_global_bgr_buffer = (uint8_t*) malloc(num_bytes * sizeof(uint8_t));
		uint8_t* bgr_buffer[8] = {*p_global_bgr_buffer};

		sws_scale(*swsContext, (const uint8_t* const*)frame->data, frame->linesize, 0, frame->height, bgr_buffer, linesize);
		// sws_freeContext(swsContext);
}

static void convertToRgb(struct SwsContext** swsContext,const struct AVFrame *frame,uint8_t** p_global_bgr_buffer) {
		if (*swsContext == NULL) {
			*swsContext = sws_getContext(frame->width, frame->height, AV_PIX_FMT_YUV420P,
				frame->width, frame->height, AV_PIX_FMT_RGB24,SWS_BILINEAR, 0, 0, 0);
			printf("--------------------------------------------------");
		}
 			

		int linesize[8] = {frame->linesize[0] * 3};
		int num_bytes = av_image_get_buffer_size(AV_PIX_FMT_RGB24, frame->width, frame->height, 1);

		if (!*p_global_bgr_buffer) {
			*p_global_bgr_buffer = (uint8_t*)MY_ALLOC(num_bytes * sizeof(uint8_t));
            memset(*p_global_bgr_buffer, 0, num_bytes * sizeof(uint8_t));
		}
		// *p_global_bgr_buffer = (uint8_t*) malloc(num_bytes * sizeof(uint8_t));
		uint8_t* bgr_buffer[8] = {*p_global_bgr_buffer};

		sws_scale(*swsContext, (const uint8_t* const*)frame->data, frame->linesize, 0, frame->height, bgr_buffer, linesize);
		// sws_freeContext(swsContext);
}

static void convertToRgba(struct SwsContext** swsContext,const struct AVFrame *frame,uint8_t** p_global_bgr_buffer) {
		if (*swsContext == NULL) {
			*swsContext = sws_getContext(frame->width, frame->height, AV_PIX_FMT_YUV420P,
				frame->width, frame->height, AV_PIX_FMT_RGBA,0, 0, 0, 0);
			printf("--------------------------------------------------");
		}
			

		int linesize[8] = {frame->linesize[0] * 4};
		int num_bytes = av_image_get_buffer_size(AV_PIX_FMT_RGBA, frame->width, frame->height, 1);

		if (!*p_global_bgr_buffer) {
			*p_global_bgr_buffer = (uint8_t*)MY_ALLOC(num_bytes * sizeof(uint8_t));
		}
		// *p_global_bgr_buffer = (uint8_t*) malloc(num_bytes * sizeof(uint8_t));
		uint8_t* bgr_buffer[8] = {*p_global_bgr_buffer};

		sws_scale(*swsContext, (const uint8_t* const*)frame->data, frame->linesize, 0, frame->height, bgr_buffer, linesize);
		// sws_freeContext(swsContext);
}

static int wrap_avcodec_decode_video4(struct SwsContext** swsContext,AVCodecContext *ctx, AVFrame *frame,uint8_t** p_global_bgr_buffer, void *data, int size) {
	struct AVPacket pkt = {.data = data, .size = size};
	char errstr[128];
	// 发送待解码的视频数据包
	int ret = avcodec_send_packet(ctx, &pkt);
	if (ret < 0) {
		av_strerror(ret, errstr, sizeof(errstr));
		printf("send_packet error %d %s\n",ret,errstr);
		return ret;
	}
	// 接收解码后的视频帧
	while (ret >= 0) {
		ret = avcodec_receive_frame(ctx, frame);
		if (ret == AVERROR(EAGAIN) || ret == AVERROR_EOF) {
			av_strerror(ret, errstr, sizeof(errstr));
			printf("avcodec_receive_frame EOF %d %s\n",ret,errstr);
			return 1;
		} else if (ret < 0) {
			av_strerror(ret, errstr, sizeof(errstr));
			printf("Error during decoding %d %s\n",ret,errstr);
			// avcodec_flush_buffers(ctx);
			return ret;
		}

		// printf("decode frame %3d\n", ctx->frame_number);
		// convertToBgr(swsContext,frame,p_global_bgr_buffer);
        convertToRgba(swsContext,frame,p_global_bgr_buffer);
		break;
	}
	return ret;
}
