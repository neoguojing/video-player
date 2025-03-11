package codec

import (
	"time"

	"videoplayer/joy4/av"
	"videoplayer/joy4/codec/fake"
)

type PCMUCodecData struct {
	typ        av.CodecType
	sampleRate int
}

func (self PCMUCodecData) Type() av.CodecType {
	return self.typ
}

func (self PCMUCodecData) SampleRate() int {
	return self.sampleRate
}

func (self PCMUCodecData) ChannelLayout() av.ChannelLayout {
	return av.CH_MONO
}

func (self PCMUCodecData) SampleFormat() av.SampleFormat {
	return av.S16
}

func (self PCMUCodecData) PacketDuration(data []byte) (time.Duration, error) {
	return time.Duration(len(data)) * time.Second / time.Duration(8000), nil
}

func NewPCMMulawCodecData(sampleRate int) av.AudioCodecData {
	return PCMUCodecData{
		typ:        av.PCM_MULAW,
		sampleRate: sampleRate,
	}
}

func NewPCMAlawCodecData(sampleRate int) av.AudioCodecData {
	return PCMUCodecData{
		typ:        av.PCM_ALAW,
		sampleRate: sampleRate,
	}
}

type SpeexCodecData struct {
	fake.CodecData
}

func (self SpeexCodecData) PacketDuration(data []byte) (time.Duration, error) {
	// libavcodec/libspeexdec.c
	// samples = samplerate/50
	// duration = 0.02s
	return time.Millisecond * 20, nil
}

func NewSpeexCodecData(sr int, cl av.ChannelLayout) SpeexCodecData {
	codec := SpeexCodecData{}
	codec.CodecType_ = av.SPEEX
	codec.SampleFormat_ = av.S16
	codec.SampleRate_ = sr
	codec.ChannelLayout_ = cl
	return codec
}

// video

type FFMPEGVideoCodecData struct {
	ty        av.CodecType
	width     int
	height    int
	ExtraData []byte
}

func (self FFMPEGVideoCodecData) Type() av.CodecType {
	return self.ty
}

func (self FFMPEGVideoCodecData) Width() int {
	return self.width
}

func (self FFMPEGVideoCodecData) Height() int {
	return self.height
}

func NewFFMPEGVideoCodecData(ty av.CodecType, width, height int, record []byte) FFMPEGVideoCodecData {
	// TODO parse width heignt
	return FFMPEGVideoCodecData{
		ty:        ty,
		width:     width,
		height:    height,
		ExtraData: record,
	}
}
