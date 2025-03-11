package hls

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"videoplayer/joy4/av"
	"videoplayer/joy4/format/rtsp/sdp"
	"videoplayer/joy4/format/ts"
	"videoplayer/joy4/log"

	"github.com/golang/groupcache/lru"
	"github.com/grafov/m3u8"
)

type dlReq struct {
	uri             string
	startDuration   time.Duration
	seqID           int64
	duration        time.Duration
	programDateTime time.Time
}

type Client struct {
	uri       string
	client    *http.Client
	segClient *http.Client
	// used in list download goroutine
	cache             *lru.Cache
	recDuration       time.Duration
	firstPktTimestamp time.Duration
	lastListUpdate    time.Time
	lastListChanged   bool
	lastSegDur        time.Duration

	// used in segment download goroutine
	startTime    time.Time
	hasCodecData bool

	chDl    chan dlReq
	chClose chan struct{}

	chOut    chan interface{}
	chStream chan []av.CodecData

	// demux *ts.Demuxer
	// streams []av.CodecData
	options Options

	// preSleepTime  time.Time
	// prePacketTime time.Duration
}

type URIRefresher func(oldUri string) (string, error)

type Options struct {
	LiveMode           bool
	LiveKeepOldSegment int
	Timeout            time.Duration
	InsecureSkipVerify bool
	URIRefresher       URIRefresher
}

const (
	maxM3U8Size             = 1 * 1024 * 1024
	defaultTimeout          = 30 * time.Second
	defaultDownloadRetry    = 3
	defaultKeepLiveSegments = 3
)

var (
	ErrTimeout        = errors.New("hls timeout")
	ErrListNotUpdated = errors.New("hls list not updated")
	ErrClosed         = errors.New("hls closed")

	errListClosed = errors.New("hls list closed")
)

func DialWithOptions(uri string, options Options) (*Client, error) {
	if options.Timeout == 0 {
		options.Timeout = defaultTimeout
	}
	if options.LiveKeepOldSegment == 0 {
		options.LiveKeepOldSegment = defaultKeepLiveSegments
	}
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: options.InsecureSkipVerify},
	}
	client := &http.Client{Timeout: options.Timeout, Transport: tr}
	maxSegments := 128
	outBufSize := 2048
	if options.LiveMode {
		maxSegments = options.LiveKeepOldSegment
		outBufSize = maxSegments * outBufSize
	}
	c := &Client{
		uri:               uri,
		client:            client,
		firstPktTimestamp: -1,
		lastListUpdate:    time.Now(),

		// XXX(chenyh) the follow 3 buffer size depends on stream params
		cache: lru.New(maxSegments),
		chDl:  make(chan dlReq, maxSegments),
		chOut: make(chan interface{}, outBufSize),

		chStream: make(chan []av.CodecData, 1),
		chClose:  make(chan struct{}),
		options:  options,
	}
	go c.pollListLoop()
	return c, nil
}

func (c *Client) Close() error {
	close(c.chClose)
	return nil
}

func (c *Client) pollList() (targetDur time.Duration, err error) {
	c.lastListChanged = false

	log.Log(log.TRACE, "hls poll list: ", c.uri)
	puri, err := url.Parse(c.uri)
	if err != nil {
		return 0, err
	}

	var resp *http.Response
	resp, err = c.client.Get(c.uri)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download HLS list error: %v", resp.StatusCode)
	}
	pl, lt, err := m3u8.DecodeFrom(io.LimitReader(resp.Body, maxM3U8Size), false)
	if err != nil {
		return 0, err
	}
	if lt != m3u8.MEDIA {
		return 0, fmt.Errorf("bad m3u8 list type: %v", lt)
	}

	mpl := pl.(*m3u8.MediaPlaylist)
	for i, v := range mpl.Segments {
		if v == nil {
			continue
		}
		dur := time.Duration(v.Duration * float64(time.Second))
		var msURI string
		if strings.HasPrefix(v.URI, "http") {
			msURI, err = url.QueryUnescape(v.URI)
			if err != nil {
				log.Log(log.WARN, "hls failed to unescape uri: ", err)
				continue
			}
		} else {
			msURL, err := puri.Parse(v.URI)
			if err != nil {
				log.Log(log.WARN, "hls invalid relative uri: ", err)
				continue
			}
			msURI, err = url.QueryUnescape(msURL.String())
			if err != nil {
				log.Log(log.WARN, "hls failed to unescape uri: ", err)
				continue
			}
		}
		log.Log(log.TRACE, "m3u8 list: ", msURI)

		urtTmp, err := url.Parse(msURI)
		if err != nil {
			log.Logf(log.WARN, "hls msURI parse failed to unescape uri: %v, err: %v", msURI, err)
			continue
		}
		files := strings.Split(urtTmp.Path, "/")
		file := files[len(files)-1]

		_, hit := c.cache.Get(file)
		if !hit {
			c.cache.Add(file, nil)
			/*
				useLocalTime := false
				if useLocalTime {
					recDuration = time.Now().Sub(startTime)
				} else {
			*/
			// }
			// TODO timeout
			no := int64(mpl.SeqNo) + int64(i)
			startDur := c.recDuration
			req := dlReq{
				uri:           msURI,
				startDuration: startDur,
				seqID:         no,
				duration:      dur,
				// programDateTime: v.ProgramDateTime,
			}
			c.recDuration += dur

			if c.options.LiveMode {
				select {
				case c.chDl <- req:
				default:
					<-c.chDl
					c.chDl <- req
					log.Log(log.WARN, "hls drop old segments: ", c.uri)
				}
			} else {
				c.chDl <- req
			}
			c.lastListUpdate = time.Now()
			c.lastListChanged = true
			c.lastSegDur = dur
		}
	}
	targetDur = time.Duration(mpl.TargetDuration * float64(time.Second))
	if mpl.Closed {
		log.Log(log.INFO, "m3u8 closed")
		return targetDur, errListClosed
	}

	return targetDur, err
}

func (c *Client) getTimerInterval() (interval time.Duration) {
	// refer https://github.com/FFmpeg/FFmpeg/blob/master/libavformat/hls.c#L1338
	if c.lastListChanged {
		interval = c.lastSegDur
	} else {
		/* If we need to reload the playlist again below (if
		 * there's still no more segments), switch to a reload
		 * interval of half the target duration. */
		interval = c.lastSegDur / 2
	}

	if interval < time.Second {
		interval = time.Second
	}
	return
}

func (c *Client) pollListLoop() {
	var err error
	var timer *time.Timer
	var closed bool

	log.Logf(log.INFO, "start hls uri:%v poll list loop", c.uri)
	targetDur, err := c.pollList()

	if err == errListClosed {
		err = nil
		closed = true
	}
	segTO := targetDur
	retry := 0
	if err != nil {
		close(c.chStream)
		close(c.chOut)
		goto done
	}
	if segTO <= 0 {
		log.Log(log.ERROR, "hls EXT-X-TARGETDURATION invalid: ", segTO)
	}
	if segTO <= 0 || (!c.options.LiveMode && segTO < c.options.Timeout) {
		segTO = c.options.Timeout
	}
	c.segClient = &http.Client{Timeout: segTO, Transport: c.client.Transport}
	go c.downloadSegments()

	if closed {
		log.Log(log.INFO, "hls closed: ", c.uri)
		goto done
	}

	timer = time.NewTimer(c.getTimerInterval())
	defer timer.Stop()

dlLoop:
	for {
		select {
		case <-c.chClose:
			log.Log(log.INFO, "hls closed")
			break dlLoop
		case <-timer.C:
			timer.Reset(c.getTimerInterval())

			if c.options.URIRefresher != nil {
				var newURI string
				newURI, err = c.options.URIRefresher(c.uri)
				if err != nil {
					break dlLoop
				}
				if c.uri != newURI {
					log.Log(log.TRACE, "hls uri refreshed: ", newURI)
				}
				c.uri = newURI
			}
			_, err = c.pollList()
			// TODO retry
			if err != nil {
				retry++
				log.Log(log.ERROR, "poll list error: ", err, ", retry: ", retry)
			} else {
				retry = 0
			}
			if retry >= defaultDownloadRetry {
				break dlLoop
			}
			if time.Now().Sub(c.lastListUpdate) > c.options.Timeout {
				log.Log(log.ERROR, "hls live list not updated after ", c.options.Timeout)
				err = ErrListNotUpdated
				break dlLoop
			}
		}
	}
done:
	if err != nil {
		log.Log(log.ERROR, "hls poll list failed: ", err)
	}
	log.Logf(log.INFO, "hls uri:%v poll list loop done", c.uri)
	close(c.chDl)
}

func (c *Client) doSegment(req dlReq) error {
	/*
		if c.options.LiveMode {
			startTime := c.startTime.Add(req.startDuration)
			if !req.programDateTime.IsZero() {
				startTime = req.programDateTime
				if time.Now().After(req.programDateTime.Add(req.duration)) {
					log.Log(log.ERROR, "ts segment out-of-date, skipping: ", req.programDateTime, req.uri)
					return nil
				}
			}
			// XXX remove this logic??
			// protect from super long live buffer served by server
			const maxAheadDownloadTime = 30 * time.Second
			earlistDownloadTime := startTime.Add(-maxAheadDownloadTime)
			diff := earlistDownloadTime.Sub(time.Now())
			if diff > 0 {
				log.Log(log.DEBUG, "hls download wait for live: ", diff)
				timer := time.NewTimer(diff)
				defer timer.Stop()
				select {
				case <-c.chClose:
					return ErrClosed
				case <-timer.C:
				}
			}
			// downloadDur := time.Now().Sub(c.startTime)
		}
	*/

	first := c.startTime.IsZero()
	resp, err := c.segClient.Get(req.uri)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download HLS segment error: %v", resp.StatusCode)
	}
	demuxer := ts.NewDemuxer(resp.Body)
	streams, err := demuxer.Streams()
	if err != nil {
		log.Log(log.INFO, "ts stream error: ", err)
		return err
	}

	if !c.hasCodecData {
		c.chStream <- streams
		if first {
			c.startTime = time.Now()
		}
		log.Log(log.DEBUG, "hls streams: ", len(streams), ", data: ", streams)
		c.hasCodecData = true
	}

	// startDL := time.Now()
	// fmt.Println("XX ", req.startDuration, req.duration, c.startTime)
	playEnd := c.startTime.Add(req.startDuration + req.duration)
	if !req.programDateTime.IsZero() {
		playEnd = req.programDateTime.Add(req.duration)
	}
	for {
		if c.options.LiveMode && time.Now().After(playEnd) {
			log.Log(log.WARN, "ts download too slow for: ", req.uri)
			return nil
		}
		pkt, err := demuxer.ReadPacket()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Log(log.INFO, "ts stream read packet error: ", err)
			return err
		}

		if c.firstPktTimestamp < 0 {
			c.firstPktTimestamp = pkt.Time
		}
		if c.options.LiveMode {
			if len(c.chOut)*3/2 >= cap(c.chOut) {
				time.Sleep(500 * time.Millisecond)
			}
			select {
			case c.chOut <- pkt:
			default:
				// XXX(chenyh): better backoff and flow control ??
				<-c.chOut
				c.chOut <- pkt
				log.Log(log.WARN, "ts packet consume too slow, dropping packet")
				time.Sleep(50 * time.Millisecond)
			}
		} else {
			c.chOut <- pkt
		}
	}
	return nil
}

func (c *Client) downloadSegments() {
	totalRetry := 0
dlLoop:
	for {
		select {
		case <-c.chClose:
			break dlLoop
		case req, ok := <-c.chDl:
			if !ok {
				break dlLoop
			}
			var err error
			for i := 0; i < 3; i++ {
				log.Log(log.DEBUG, "downloading ", req, ", retry: ", i)
				err = c.doSegment(req)
				if err == nil {
					break
				}
			}
			if err != nil {
				log.Log(log.ERROR, "failed to download: ", req, ", total retry: ", totalRetry)
				totalRetry++
			} else {
				totalRetry = 0
			}
			if totalRetry >= defaultDownloadRetry {
				c.chOut <- err
				break dlLoop
			}
		}
	}
	log.Log(log.DEBUG, "downloadSegments done")
	close(c.chStream)
	close(c.chOut)
}

func (c *Client) ReadPacket() (av.Packet, error) {

	data, ok := <-c.chOut
	if !ok {
		log.Log(log.TRACE, "hls read packet eof")
		return av.Packet{}, io.EOF
	}

	var pkt av.Packet
	switch v := data.(type) {
	case av.Packet:
		pkt = v
	default:
		return av.Packet{}, v.(error)
	}

	// fmt.Printf("pkt.time:%v\n", pkt.Time)
	// if c.preSleepTime.IsZero() {
	// 	c.preSleepTime = time.Now()
	// 	c.prePacketTime = 0
	// }

	// // flow control
	// if c.options.LiveMode {
	// 	st := time.Second / 2
	// 	frameTime := pkt.Time - c.prePacketTime
	// 	c.prePacketTime = pkt.Time
	// 	dt := time.Until(c.preSleepTime.Add(frameTime))
	// 	if dt < 0 {
	// 		c.preSleepTime = time.Now()
	// 		return pkt, nil
	// 	} else if dt > 0 && dt < st {
	// 		st = dt
	// 	}
	// 	time.Sleep(st)
	// 	// fmt.Printf("t:%v\n", st)
	// 	c.preSleepTime = time.Now()
	// }

	// l := len(pkt.Data)
	// if l > 8 {
	// 	l = 8
	// }
	// if pkt.Idx == 0 {
	// 	fmt.Println(pkt.Time, "   ", len(pkt.Data), hex.Dump(pkt.Data[:l]))
	// }

	return pkt, nil
}

func (c *Client) Streams() (streams []av.CodecData, err error) {
	to := time.NewTimer(10 * time.Second)
	defer to.Stop()
	select {
	case s, ok := <-c.chStream:
		if !ok {
			return nil, io.ErrUnexpectedEOF
		}
		return s, nil
	case <-to.C:
		return nil, ErrTimeout
	}
}

func (c *Client) SDP() (sdp sdp.SDPInfo, err error) {
	var streams []av.CodecData
	streams, err = c.Streams()
	if err != nil {
		return
	}
	sdp.CodecDatas = streams
	return
}
