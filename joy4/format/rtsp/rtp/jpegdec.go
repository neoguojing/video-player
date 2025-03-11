package rtp

import (
	"bytes"

	"videoplayer/joy4/av"
	"videoplayer/joy4/codec/mjpegparser"
	"videoplayer/joy4/format/rtsp/sdp"
	"videoplayer/joy4/log"

	"github.com/nareix/bits/pio"
)

const JPEG_MAX_FRAME_SIZE = 5 << 20

var jpegEndMarker = [2]byte{0xff, 0xd9}
var defaultQ = [128]byte{
	/* luma table */
	16, 11, 12, 14, 12, 10, 16, 14,
	13, 14, 18, 17, 16, 19, 24, 40,
	26, 24, 22, 22, 24, 49, 35, 37,
	29, 40, 58, 51, 61, 60, 57, 51,
	56, 55, 64, 72, 92, 78, 64, 68,
	87, 69, 55, 56, 80, 109, 81, 87,
	95, 98, 103, 104, 103, 62, 77, 113,
	121, 112, 100, 120, 92, 101, 103, 99,

	/* chroma table */
	17, 18, 18, 24, 21, 24, 47, 26,
	26, 47, 99, 66, 56, 66, 99, 99,
	99, 99, 99, 99, 99, 99, 99, 99,
	99, 99, 99, 99, 99, 99, 99, 99,
	99, 99, 99, 99, 99, 99, 99, 99,
	99, 99, 99, 99, 99, 99, 99, 99,
	99, 99, 99, 99, 99, 99, 99, 99,
	99, 99, 99, 99, 99, 99, 99, 99,
}

/* Set up the standard Huffman tables (cf. JPEG standard section K.3) */
/* IMPORTANT: these are only valid for 8-bit data precision! */
var avpriv_mjpeg_bits_dc_luminance = [17]byte{ /* 0-base */ 0, 0, 1, 5, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0}
var avpriv_mjpeg_val_dc = [12]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}

var avpriv_mjpeg_bits_dc_chrominance = [17]byte{ /* 0-base */ 0, 0, 3, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0}

var avpriv_mjpeg_bits_ac_luminance = [17]byte{ /* 0-base */ 0, 0, 2, 1, 3, 3, 2, 4, 3, 5, 5, 4, 4, 0, 0, 1, 0x7d}

var avpriv_mjpeg_val_ac_luminance = []byte{0x01, 0x02, 0x03, 0x00, 0x04, 0x11, 0x05, 0x12,
	0x21, 0x31, 0x41, 0x06, 0x13, 0x51, 0x61, 0x07,
	0x22, 0x71, 0x14, 0x32, 0x81, 0x91, 0xa1, 0x08,
	0x23, 0x42, 0xb1, 0xc1, 0x15, 0x52, 0xd1, 0xf0,
	0x24, 0x33, 0x62, 0x72, 0x82, 0x09, 0x0a, 0x16,
	0x17, 0x18, 0x19, 0x1a, 0x25, 0x26, 0x27, 0x28,
	0x29, 0x2a, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39,
	0x3a, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49,
	0x4a, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59,
	0x5a, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69,
	0x6a, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79,
	0x7a, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89,
	0x8a, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98,
	0x99, 0x9a, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7,
	0xa8, 0xa9, 0xaa, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6,
	0xb7, 0xb8, 0xb9, 0xba, 0xc2, 0xc3, 0xc4, 0xc5,
	0xc6, 0xc7, 0xc8, 0xc9, 0xca, 0xd2, 0xd3, 0xd4,
	0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda, 0xe1, 0xe2,
	0xe3, 0xe4, 0xe5, 0xe6, 0xe7, 0xe8, 0xe9, 0xea,
	0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8,
	0xf9, 0xfa,
}

var avpriv_mjpeg_bits_ac_chrominance = [17]byte{ /* 0-base */ 0, 0, 2, 1, 2, 4, 4, 3, 4, 7, 5, 4, 4, 0, 1, 2, 0x77}

var avpriv_mjpeg_val_ac_chrominance = []byte{0x00, 0x01, 0x02, 0x03, 0x11, 0x04, 0x05, 0x21,
	0x31, 0x06, 0x12, 0x41, 0x51, 0x07, 0x61, 0x71,
	0x13, 0x22, 0x32, 0x81, 0x08, 0x14, 0x42, 0x91,
	0xa1, 0xb1, 0xc1, 0x09, 0x23, 0x33, 0x52, 0xf0,
	0x15, 0x62, 0x72, 0xd1, 0x0a, 0x16, 0x24, 0x34,
	0xe1, 0x25, 0xf1, 0x17, 0x18, 0x19, 0x1a, 0x26,
	0x27, 0x28, 0x29, 0x2a, 0x35, 0x36, 0x37, 0x38,
	0x39, 0x3a, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48,
	0x49, 0x4a, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58,
	0x59, 0x5a, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68,
	0x69, 0x6a, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
	0x79, 0x7a, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87,
	0x88, 0x89, 0x8a, 0x92, 0x93, 0x94, 0x95, 0x96,
	0x97, 0x98, 0x99, 0x9a, 0xa2, 0xa3, 0xa4, 0xa5,
	0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xb2, 0xb3, 0xb4,
	0xb5, 0xb6, 0xb7, 0xb8, 0xb9, 0xba, 0xc2, 0xc3,
	0xc4, 0xc5, 0xc6, 0xc7, 0xc8, 0xc9, 0xca, 0xd2,
	0xd3, 0xd4, 0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda,
	0xe2, 0xe3, 0xe4, 0xe5, 0xe6, 0xe7, 0xe8, 0xe9,
	0xea, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8,
	0xf9, 0xfa,
}

type JPEGDynamicProtocol struct {
	hasCodecData bool
	codecData    av.VideoCodecData

	hdrSize   int
	qTables   [128][128]byte
	qTableLen [128]uint16

	frame []byte

	timestamp uint32
}

func (h *JPEGDynamicProtocol) Type() av.CodecType {
	if h.hasCodecData {
		return h.codecData.Type()
	}
	return av.VIDEO_UNKNOWN
}

func (h *JPEGDynamicProtocol) ParseSDP(sdp *sdp.Media) error {
	return nil
}

func (h *JPEGDynamicProtocol) DefaultTimeScale() int {
	return 90000
}

func createDefaultQTables(q byte) [128]byte {
	factor := int(q)
	var s uint16
	if q < 50 {
		s = uint16(5000 / factor)
	} else {
		s = uint16(200 - factor*2)
	}
	var r [128]byte
	for i := 0; i < 128; i++ {
		val := (uint16(defaultQ[i])*s + 50) / 100
		/* Limit the quantizers to 1 <= q <= 255. */
		if val < 1 {
			val = 1
		}
		if val > 255 {
			val = 255
		}
		r[i] = byte(val)
	}
	return r
}

func jpegPutMarker(b *bytes.Buffer, code byte) {
	b.WriteByte(0xff)
	b.WriteByte(code)
}

func jpegPutBE16(b *bytes.Buffer, v uint16) {
	b.WriteByte(byte(v >> 8))
	b.WriteByte(byte(v & 0xff))
}

func createJPEGHuffmanTable(class int, tableID int, bits []byte, value []byte) []byte {
	b := bytes.NewBuffer(nil)
	n := 0
	b.WriteByte(byte((class << 4) | tableID))
	for i := 1; i <= 16; i++ {
		n += int(bits[i])
		b.WriteByte(bits[i])
	}
	for i := 0; i < n; i++ {
		b.WriteByte(value[i])
	}
	return b.Bytes()
}

func createJPEGHeader(ty byte, width int, height int, qtable []byte, nbQtable int, dri uint16) []byte {
	b := bytes.NewBuffer(nil)
	/* Convert from blocks to pixels. */
	width <<= 3
	height <<= 3
	/* SOI */
	jpegPutMarker(b, 0xd8)
	/* JFIF header */
	jpegPutMarker(b, 0xe0) // APP0
	jpegPutBE16(b, 16)
	b.WriteByte(byte('J'))
	b.WriteByte(byte('F'))
	b.WriteByte(byte('I'))
	b.WriteByte(byte('F'))
	b.WriteByte(0)
	jpegPutBE16(b, 0x0102)
	b.WriteByte(0)
	jpegPutBE16(b, 1)
	jpegPutBE16(b, 1)
	b.WriteByte(0)
	b.WriteByte(0)

	if dri != 0 {
		// DRI
		jpegPutMarker(b, 0xdd)
		jpegPutBE16(b, 4)
		jpegPutBE16(b, dri)
	}

	/* DQT */
	jpegPutMarker(b, 0xdb)
	jpegPutBE16(b, uint16(2+nbQtable*(1+64)))

	for i := 0; i < int(nbQtable); i++ {
		b.WriteByte(byte(i))
		b.Write(qtable[64*i : 64*(i+1)])
	}

	/* DHT */
	jpegPutMarker(b, 0xc4)
	t1 := createJPEGHuffmanTable(0, 0, avpriv_mjpeg_bits_dc_luminance[:], avpriv_mjpeg_val_dc[:])
	t2 := createJPEGHuffmanTable(0, 1, avpriv_mjpeg_bits_dc_chrominance[:], avpriv_mjpeg_val_dc[:])
	t3 := createJPEGHuffmanTable(1, 0, avpriv_mjpeg_bits_ac_luminance[:], avpriv_mjpeg_val_ac_luminance[:])
	t4 := createJPEGHuffmanTable(1, 1, avpriv_mjpeg_bits_ac_chrominance[:], avpriv_mjpeg_val_ac_chrominance[:])
	jpegPutBE16(b, uint16(2+len(t1)+len(t2)+len(t3)+len(t4)))
	b.Write(t1)
	b.Write(t2)
	b.Write(t3)
	b.Write(t4)

	/* SOF0 */
	jpegPutMarker(b, 0xc0)
	jpegPutBE16(b, 17) /* size */
	b.WriteByte(8)     /* bits per component */
	jpegPutBE16(b, uint16(height))
	jpegPutBE16(b, uint16(width))
	b.WriteByte(3) /* number of components */
	b.WriteByte(1) /* component number */
	sam := 1
	if ty > 0 {
		sam = 2
	}
	matrix := 0
	if nbQtable == 2 {
		matrix = 1
	}

	b.WriteByte(byte((2 << 4) | sam)) /* hsample/vsample */
	b.WriteByte(0)                    /* matrix number */
	b.WriteByte(2)                    /* component number */
	b.WriteByte((1 << 4) | 1)         /* hsample/vsample */
	b.WriteByte(byte(matrix))         /* matrix number */
	b.WriteByte(3)                    /* component number */
	b.WriteByte((1 << 4) | 1)         /* hsample/vsample */
	b.WriteByte(byte(matrix))         /* matrix number */

	/* SOS */
	jpegPutMarker(b, 0xda)
	jpegPutBE16(b, 12)
	b.WriteByte(3)
	b.WriteByte(1)
	b.WriteByte(0)
	b.WriteByte(2)
	b.WriteByte(17)
	b.WriteByte(3)
	b.WriteByte(17)
	b.WriteByte(0)
	b.WriteByte(63)
	b.WriteByte(0)

	return b.Bytes()
}

func (h *JPEGDynamicProtocol) ParsePacket(pkt *av.Packet, buf []byte, timestamp uint32, flags int) (uint32, int) {
	if len(buf) < 8 {
		log.Log(log.ERROR, "Too short RTP/JPEG packet.")
		return timestamp, -1
	}

	/* Parse the main JPEG header. */

	/* fragment byte offset */
	off := pio.U24BE(buf[1:])
	/* id of jpeg decoder params */
	ty := pio.U8(buf[4:])
	/* quantization factor (or table id) */
	q := pio.U8(buf[5:])
	width := pio.U8(buf[6:])
	height := pio.U8(buf[7:])
	buf = buf[8:]
	dri := uint16(0)
	if ty&0x40 != 0 {
		if len(buf) < 4 {
			log.Log(log.ERROR, "Too short RTP/JPEG packet.")
			return timestamp, -1
		}
		dri = pio.U16BE(buf)
		buf = buf[4:]
		ty &= ^0x40 & 0xff
	}
	if ty > 1 {
		log.Logf(log.ERROR, "MJPEG RTP/JPEG type %v, size %dx%d", ty, width, height)
		return timestamp, -2
	}
	/* Parse the quantization table header. */

	var qtables []byte
	if off == 0 {
		/* Start of JPEG data packet. */
		if q > 127 {
			if len(buf) < 4 {
				log.Log(log.ERROR, "Too short RTP/JPEG packet.")
				return timestamp, -1
			}
			precision := pio.U8(buf[1:])
			qTableLen := pio.U16BE(buf[2:])
			buf = buf[4:]
			if precision != 0 {
				log.Log(log.WARN, "Only 8-bit precision is supported.")
			}

			if qTableLen > 0 {
				if len(buf) < int(qTableLen) {
					log.Log(log.ERROR, "Too short RTP/JPEG packet.")
					return timestamp, -1
				}
				qtables = buf[:qTableLen]
				buf = buf[qTableLen:]
				if q < 255 {
					if h.qTableLen[q-128] != 0 &&
						(h.qTableLen[q-128] != qTableLen || bytes.Compare(qtables, h.qTables[q-128][:]) != 0) {
						log.Logf(log.WARN, "Quantization tables for q=%d changed", q)
					} else if h.qTableLen[q-128] == 0 && qTableLen <= 128 {
						copy(h.qTables[q-128][:], qtables[:qTableLen])
						h.qTableLen[q-128] = qTableLen
					}
				}
			} else {
				if q == 255 {
					log.Log(log.ERROR, "Invalid RTP/JPEG packet. Quantization tables not found.")
					return timestamp, -1
				}
				if h.qTableLen[q-128] == 0 {
					log.Logf(log.ERROR, "No quantization tables known for q=%d yet.", q)
					return timestamp, -1
				}
				qtables = h.qTables[q-128][:h.qTableLen[q-128]]
			}
		} else { // q <= 127
			if q == 0 || q > 99 {
				log.Logf(log.ERROR, "Reserved q value %d", q)
				return timestamp, -1
			}
			newQ := createDefaultQTables(q)
			qtables = newQ[:]
		}

		h.frame = nil
		h.timestamp = timestamp
		/* Generate a frame and scan headers that can be prepended to
		* the
		* RTP/JPEG data payload to produce a JPEG compressed
		* image in
		* interchange format. */
		hdr := createJPEGHeader(ty, int(width), int(height), qtables, len(qtables)/64, dri)
		h.hdrSize = len(hdr)
		h.frame = append(h.frame, hdr...)
		if !h.hasCodecData {
			realW := int(width) << 3
			realH := int(height) << 3
			//h.codecData = codec.NewFFMPEGVideoCodecData(av.MJPEG, realW, realH, nil)
			h.codecData = mjpegparser.NewMJPEGCodecData(av.MJPEG, realW, realH, nil)
			h.hasCodecData = true
		}
	}

	if len(h.frame) == 0 {
		log.Log(log.ERROR, "Received packet without a start chunk; dropping frame.")
		return timestamp, -1
	}

	if h.timestamp != timestamp {
		/* Skip the current frame if timestamp is incorrect.
		 * A start packet has been lost somewhere. */
		h.frame = nil
		log.Log(log.ERROR, "RTP timestamps don't match.")
		return timestamp, -1
	}
	if int(off) != len(h.frame)-h.hdrSize {
		log.Log(log.ERROR,
			"Missing packets; dropping frame.")
		return timestamp, -1
	}

	if len(h.frame)+len(buf) > JPEG_MAX_FRAME_SIZE {
		log.Log(log.ERROR,
			"Frame too large; dropping frame.")
		return timestamp, -1
	}
	h.frame = append(h.frame, buf...)
	if flags&RTP_FLAG_MARKER != 0 {
		/* End of JPEG data packet. */
		h.frame = append(h.frame, jpegEndMarker[:]...)
		pkt.Data = h.frame
		pkt.MediaCodecType = h.Type()
		h.frame = nil
		return timestamp, 0
	}
	return timestamp, -1
}

func (h *JPEGDynamicProtocol) PayloadType() int {
	return 26
}

func (h *JPEGDynamicProtocol) CodecData() av.CodecData {
	if h.hasCodecData {
		return h.codecData
	}
	return nil
}

func (h *JPEGDynamicProtocol) SDPLines() []string {
	return nil
}
