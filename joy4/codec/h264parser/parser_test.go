package h264parser

import (
	"encoding/hex"
	"reflect"
	"testing"
)

func TestParser(t *testing.T) {
	var ty int
	var nalus [][]byte

	annexbFrame, _ := hex.DecodeString("00000001223322330000000122332233223300000133000001000001")
	nalus, ty = GuessNALUType(annexbFrame)
	t.Log(ty, len(nalus))

	avccFrame, _ := hex.DecodeString(
		"00000008aabbccaabbccaabb00000001aa",
	)
	nalus, ty = GuessNALUType(avccFrame)
	t.Log(ty, len(nalus))
}

func TestSEI(t *testing.T) {
	data, _ := hex.DecodeString("00ac43000001ff")
	sei := SEIMessage{
		Type:        SEI_TYPE_USER_DATA_UNREGISTERED,
		Payload:     data,
		PayloadSize: uint(len(data)),
	}

	nal, err := sei.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(hex.Dump(nal.Raw))
	nal1, _ := ExtractRBSP(nal.Raw, true)
	sei1, err := ParseSEIMessageFromNALU(nal1.Rbsp)
	if err != nil {
		t.Fatal(err)
	}
	if sei1.PayloadSize != sei.PayloadSize {
		t.Error("size mismatch:", sei1.PayloadSize)
	}
	if !reflect.DeepEqual(sei.Payload, sei1.Payload) {
		t.Error("payload not equal")
	}
}
