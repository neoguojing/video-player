package mjpegparser

import "videoplayer/joy4/av"

const (
	/* start of frame */
	SOF0 = 0xc0 /* baseline */
	SOF1 = 0xc1 /* extended sequential, huffman */
	SOF2 = 0xc2 /* progressive, huffman */
	SOF3 = 0xc3 /* lossless, huffman */

	SOF5  = 0xc5 /* differential sequential, huffman */
	SOF6  = 0xc6 /* differential progressive, huffman */
	SOF7  = 0xc7 /* differential lossless, huffman */
	JPG   = 0xc8 /* reserved for JPEG extension */
	SOF9  = 0xc9 /* extended sequential, arithmetic */
	SOF10 = 0xca /* progressive, arithmetic */
	SOF11 = 0xcb /* lossless, arithmetic */

	SOF13 = 0xcd /* differential sequential, arithmetic */
	SOF14 = 0xce /* differential progressive, arithmetic */
	SOF15 = 0xcf /* differential lossless, arithmetic */

	DHT = 0xc4 /* define huffman tables */

	DAC = 0xcc /* define arithmetic-coding conditioning */

	/* restart with modulo 8 count "m" */
	RST0 = 0xd0
	RST1 = 0xd1
	RST2 = 0xd2
	RST3 = 0xd3
	RST4 = 0xd4
	RST5 = 0xd5
	RST6 = 0xd6
	RST7 = 0xd7

	SOI = 0xd8 /* start of image */
	EOI = 0xd9 /* end of image */
	SOS = 0xda /* start of scan */
	DQT = 0xdb /* define quantization tables */
	DNL = 0xdc /* define number of lines */
	DRI = 0xdd /* define restart interval */
	DHP = 0xde /* define hierarchical progression */
	EXP = 0xdf /* expand reference components */

	APP0  = 0xe0
	APP1  = 0xe1
	APP2  = 0xe2
	APP3  = 0xe3
	APP4  = 0xe4
	APP5  = 0xe5
	APP6  = 0xe6
	APP7  = 0xe7
	APP8  = 0xe8
	APP9  = 0xe9
	APP10 = 0xea
	APP11 = 0xeb
	APP12 = 0xec
	APP13 = 0xed
	APP14 = 0xee
	APP15 = 0xef

	JPG0  = 0xf0
	JPG1  = 0xf1
	JPG2  = 0xf2
	JPG3  = 0xf3
	JPG4  = 0xf4
	JPG5  = 0xf5
	JPG6  = 0xf6
	SOF48 = 0xf7 ///< JPEG-LS
	LSE   = 0xf8 ///< JPEG-LS extension parameters
	JPG9  = 0xf9
	JPG10 = 0xfa
	JPG11 = 0xfb
	JPG12 = 0xfc
	JPG13 = 0xfd

	COM = 0xfe /* comment */

	TEM = 0x01 /* temporary private use for arithmetic coding */

	/* 0x02 -> 0xbf reserved */
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
var Avpriv_mjpeg_bits_dc_luminance = [17]byte{ /* 0-base */ 0, 0, 1, 5, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0}
var Avpriv_mjpeg_val_dc = [12]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}

var Avpriv_mjpeg_bits_dc_chrominance = [17]byte{ /* 0-base */ 0, 0, 3, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0}

var Avpriv_mjpeg_bits_ac_luminance = [17]byte{ /* 0-base */ 0, 0, 2, 1, 3, 3, 2, 4, 3, 5, 5, 4, 4, 0, 0, 1, 0x7d}

var Avpriv_mjpeg_val_ac_luminance = []byte{0x01, 0x02, 0x03, 0x00, 0x04, 0x11, 0x05, 0x12,
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

var Avpriv_mjpeg_bits_ac_chrominance = [17]byte{ /* 0-base */ 0, 0, 2, 1, 2, 4, 4, 3, 4, 7, 5, 4, 4, 0, 1, 2, 0x77}

var Avpriv_mjpeg_val_ac_chrominance = []byte{0x00, 0x01, 0x02, 0x03, 0x11, 0x04, 0x05, 0x21,
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

type CodecData struct {
	Record []byte
	width  int
	height int
}

func (self CodecData) Type() av.CodecType {
	return av.MJPEG
}

func (self CodecData) Width() int {
	return self.width
}

func (self CodecData) Height() int {
	return self.height
}

func NewMJPEGCodecData(ty av.CodecType, width, height int, record []byte) (cd CodecData) {

	return CodecData{
		Record: record,
		width:  width,
		height: height,
	}
}
