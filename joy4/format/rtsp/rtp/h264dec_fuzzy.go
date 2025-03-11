//go:build gofuzz
// +build gofuzz

package rtp

import "videoplayer/joy4/av"

func Fuzz(data []byte) int {
	dp := NewH264DynamicProtocol(nil)
	var pkt av.Packet
	_, rv := dp.ParsePacket(&pkt, data, 0, 0)
	if rv == 1 || rv == 0 {
		return 1
	}
	return 0
}
