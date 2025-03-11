package main

import (
	"flag"
	"fmt"
	"io"
	"time"

	"videoplayer/joy4/av"
	"videoplayer/joy4/av/avutil"
	"videoplayer/joy4/format"
	"videoplayer/joy4/format/hls"
	"videoplayer/joy4/log"
)

func init() {
	format.RegisterAll()
}

func main() {
	srcfile := flag.String("src", "http://localhost:4000/playlist.m3u8", "Source file")
	dstfile := flag.String("dst", "output.flv", "Output file")
	printTrace := flag.Bool("trace", false, "Print trace")
	max := flag.Int("max", 0, "Max seconds")
	live := flag.Bool("live", false, "live mode")
	flag.Parse()

	if *printTrace {
		log.DefaultStandardLogger.Level = log.TRACE
	} else {
		log.DefaultStandardLogger.Level = log.DEBUG
	}

	src, err := hls.DialWithOptions(*srcfile, hls.Options{LiveMode: *live})
	if err != nil {
		panic(err)
	}
	defer src.Close()
	if *dstfile == "" {
		startTime := time.Now()
		fmt.Println("start streams")
		streams, err := src.Streams()
		fmt.Println("end streams")
		if err != nil {
			panic(err)
		}
		fmt.Println("streams: ", streams)
		firstPktTime := time.Duration(-1)
		lastPktTime := time.Duration(-1)
		for {
			pkt, err := src.ReadPacket()
			if err != nil {
				fmt.Println("exit: ", err)
				break
			}
			if firstPktTime < 0 {
				firstPktTime = pkt.Time
				lastPktTime = pkt.Time
			}
			if pkt.Time < firstPktTime {
				firstPktTime = pkt.Time
			}
			oldPkt := pkt.Time
			pkt.Time = pkt.Time - firstPktTime
			if lastPktTime < 0 {
				lastPktTime = pkt.Time
			}
			if pkt.Time < lastPktTime {
				fmt.Println("smaller: ", lastPktTime)
			}
			lastPktTime = pkt.Time
			fmt.Println("pkt: ", pkt.Idx, pkt.Time, oldPkt, len(pkt.Data))
			if *live {
				// SAMPLE: adjust real pts
				pts := startTime.Add(pkt.Time)
				now := time.Now()
				if pts.After(now) {
					time.Sleep(pts.Sub(now))
				}
			}
			l := 32
			if l > len(pkt.Data) {
				l = len(pkt.Data)
			}
			// fmt.Println(hex.Dump(pkt.Data[:l]))
		}
	} else {
		dst, err := avutil.Create(*dstfile)
		if err != nil {
			panic(err)
		}
		// same as calling avutil.CopyFile(dst, src) but added
		// max duration in case the src is live and never ends
		err = CopyFileMax(dst, src, time.Duration(*max)*time.Second)
		if err != nil {
			fmt.Println("Copy error: ", err)
		}
	}
}

func CopyPacketsMax(dst av.PacketWriter, src av.PacketReader, max time.Duration) (err error) {
	for {
		var pkt av.Packet
		if pkt, err = src.ReadPacket(); err != nil {
			if err == io.EOF {
				err = nil
				break
			}
			return
		}
		// fmt.Println("HEREXX")

		/*
			// skip SEI/SPS/PPS
			if pkt.Idx != 0 {
				continue
			}
			if len(pkt.Data) < 5 || pkt.Data[4]&0x1f > 5 {
				fmt.Println("XXXX")
				continue
			}
		*/
		// if pkt.Idx != 0 {
		// 	continue
		// }
		// nalus := h264parser.SplitNALUs(pkt.Data, true, 4, av.H264, true)
		// fmt.Println("X ", len(nalus))
		if pkt.IsKeyFrame {
			// fmt.Println("key frame: ", pkt.Idx, pkt.Time, time.Now())
		}
		// break when max time has been reached
		if max > 0 && pkt.Time >= max {
			return
		}

		if err = dst.WritePacket(pkt); err != nil {
			return
		}
	}
	return
}

func CopyFileMax(dst av.Muxer, src av.Demuxer, max time.Duration) (err error) {
	var streams []av.CodecData
	if streams, err = src.Streams(); err != nil {
		return
	}
	fmt.Printf("codec data: %+v\n", streams)
	if err = dst.WriteHeader(streams); err != nil {
		return
	}
	if err = CopyPacketsMax(dst, src, max); err != nil {
		return
	}
	if err = dst.WriteTrailer(); err != nil {
		return
	}
	return
}
