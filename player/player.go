package player

import (
	"errors"
	"fmt"
	"net/url"
	"time"
	"videoplayer/config"

	"videoplayer/ffmpeg"
	"videoplayer/pb"

	log "github.com/sirupsen/logrus"
)

const (
	// PlayVideo 表示播放视频请求
	PlayVideo RequestType = iota
	// CloseVideo 表示关闭视频请求
	CloseVideo
	// MoveWindow 表示移动视频窗口
	MoveWindow
	// HideWindow 隐藏窗口
	HideWindow
	// ShowWindow 显示窗口
	ShowWindow
	// CloseAll 关闭所有窗口
	CloseAll
)

// RequestType 表示请求的类型
type RequestType int

// Request 表示一个请求
type Request struct {
	Type   RequestType
	Device Device
	Pos    Position
	Err    chan error
}

func NewRequest(requestType RequestType, device Device, position Position) Request {
	return Request{
		Type:   requestType,
		Device: device,
		Pos:    position,
	}
}

type frameData struct {
	id string
	// frame gocv.Mat
	frame       *ffmpeg.VideoFrame
	sei         []*pb.PreviewInfo
	receiveTime time.Time
}

type State struct {
	windowID string
	err      error
}

// Player 结构体 todo 加锁
type Player struct {
	windows     map[string]Window
	demuxers    map[string]*Demuxer
	commandChan chan Request
	frameChan   chan frameData
	stopChan    chan struct{}
	stateChan   chan State

	useOpencv bool
}

// NewPlayer 创建一个新的 Player 实例
func NewPlayer() *Player {
	return &Player{
		windows:     make(map[string]Window),
		demuxers:    make(map[string]*Demuxer),
		commandChan: make(chan Request, 10),
		frameChan:   make(chan frameData, 100),
		stopChan:    make(chan struct{}),
		stateChan:   make(chan State, 10),
		useOpencv:   config.GlobalConfig.UseOpenCV,
	}
}

func (p *Player) CommandChan() chan Request {
	return p.commandChan
}

// Run 启动播放器
func (p *Player) Run() {
	log.Info("Player is running")
	frameCount := 0
	startTime := time.Now()
	defer func() {
		endTime := time.Now()
		duration := endTime.Sub(startTime)
		fps := float64(frameCount) / duration.Seconds()

		fmt.Printf("总帧数：%d\n", frameCount)
		fmt.Printf("总时间：%v\n", duration)
		fmt.Printf("帧率：%.2f fps\n", fps)
	}()

	for {
		select {
		case <-p.stopChan:
			//todo release all dumexers and windows
			log.Info("Player stopped")
			return
		case request := <-p.commandChan:
			log.Debugf("commandChan received, request: %v", request)
			var err error
			// 处理请求
			switch request.Type {
			case PlayVideo:
				err = p.playVideo(request.Device, request.Pos)
			case CloseVideo:
				err = p.closeVideo(request.Device.ID)
			case MoveWindow:
				err = p.moveVideo(request.Device.ID, request.Pos)
			case HideWindow:
				if p.useOpencv {
					err = p.closeVideo(request.Device.ID)
				} else {
					err = p.hideVideo(request.Device.ID)
				}

			case ShowWindow:
				if p.useOpencv {
					err = p.playVideo(request.Device, request.Pos)
				} else {
					err = p.showVideo(request.Device, request.Pos)
				}

			case CloseAll:
				err = p.closeAll()
			}
			request.Err <- err
		case frame := <-p.frameChan:
			log.Debugf("frameChan received windowID: %v,%v", frame.id, len(p.frameChan))
			//log.Info("frameChan.len: %v", len(p.frameChan))
			img := frame.frame
			id := frame.id
			window := p.windows[id]
			// 如果当前窗口被关闭，但是frameChan中任存在该设备未播放的图片，此时window已经为nil，需要判断防止NPE
			if window == nil || !window.IsOpen() {
				continue
			}
			// 如果图像为空，则继续循环
			if img == nil {
				continue
			}
			frameCount++
			// 在窗口中显示图像，并等待1毫秒
			window.IMShow(img, frame.sei)
			// 不调用WaitKey不会显示画面
			deley := 1
			if !p.useOpencv {
				elapsed := time.Since(frame.receiveTime)
				if elapsed < 35*time.Millisecond {
					time.Sleep(35*time.Millisecond - elapsed)
				}
			}
			window.WaitKey(deley)
		case state := <-p.stateChan:
			// demuxer报错，重连
			if state.err != nil {
				log.Infof("stateChan received: %v,trying to recreate demuxer", state)
				go p.reconnect(state.windowID)
			}
		}
	}
}

// Stop 停止播放器
func (p *Player) Stop() {
	close(p.stopChan)
}

func replaceTokenInURL(originalURL, newToken string) (string, error) {
	parsedURL, err := url.Parse(originalURL)
	if err != nil {
		return "", err
	}

	queryValues, err := url.ParseQuery(parsedURL.RawQuery)
	if err != nil {
		return "", err
	}

	// 替换或添加新的 token 参数
	queryValues.Set("jwt", newToken)

	// 更新 URL 的 RawQuery
	parsedURL.RawQuery = queryValues.Encode()

	return parsedURL.String(), nil
}

func (p *Player) reconnect(windowID string) {
	demuxer := p.demuxers[windowID]
	demuxer.Release()

	w := p.windows[windowID]
	if w == nil {
		return
	}

	log.Info("attempt to recreate demuxer, %v", w.GetDevice())
	err := RetryFunc(func() error {
		token, err := config.GetToken()
		if err != nil {
			log.Errorf("get token failed,err:%v", err)
			return err
		}
		newUrl, _ := replaceTokenInURL(w.GetDevice().WSURL, token)
		log.Infof("get new wsurl:%v", newUrl)
		dev := w.GetDevice()
		dev.WSURL = newUrl
		dem, err := NewDemuxer(dev.WSURL, dev.RTSPURL, p.frameChan, p.stateChan, dev.ID)
		if err != nil {
			log.Errorf("create demuxer failed, err: %v", err)
			return err
		}
		if err = dem.Start(); err != nil {
			log.Errorf("demuxer start failed, dev: %v,err:%v", dev, err)
			dem.Release()
			return err
		}
		p.demuxers[dev.ID] = dem
		return nil
	}, 120, 5*time.Second)
	if err != nil {
		log.Errorf("recreate demuxer failed with 5 times :%v , err : %v", w.GetDevice(), err)
		p.closeVideo(windowID)
	}
}

// playVideo 处理播放视频请求
func (p *Player) playVideo(dev Device, pos Position) error {
	var err error
	log.Infof("Playing video for webcam %v", dev)
	dem, err := NewDemuxer(dev.WSURL, dev.RTSPURL, p.frameChan, p.stateChan, dev.ID)
	if err != nil {
		log.Errorf("create demuxer failed, err: %v", err)
		return err
	}
	if err = dem.Start(); err != nil {
		dem.Release()
		log.Errorf("demuxer start failed, dev: %v,err:%v", dev, err)
		return err
	}
	p.demuxers[dev.ID] = dem
	p.windows[dev.ID] = NewWindow(pos, dev, dem.UseOpenCV, dem.IsCuda)
	return nil
}

// closeVideo 处理关闭视频请求
func (p *Player) closeVideo(windowID string) error {
	var err error
	log.Infof("Closing video for webcam %v", windowID)
	demuxer := p.demuxers[windowID]
	window := p.windows[windowID]
	if window != nil && window.IsOpen() {
		window.Close()
		delete(p.windows, windowID)
	}
	if demuxer != nil {
		demuxer.Release()
		delete(p.demuxers, windowID)
	}
	return err
}

// closeAll 处理关闭所有视频请求
func (p *Player) closeAll() error {
	var err error
	log.Info("Closing all video")
	for windowID := range p.windows {
		p.closeVideo(windowID)
	}
	return err
}

// hideVideo 处理隐藏视频请求
func (p *Player) hideVideo(windowID string) error {
	var err error
	log.Infof("hide video for webcam %v", windowID)
	window := p.windows[windowID]
	if window == nil || !window.IsOpen() {
		return fmt.Errorf("windowID: %v not exist", windowID)
	}
	// todo 窗口取消固定最前
	window.Hide()

	return err
}

// showVideo 处理显示视频请求
func (p *Player) showVideo(dev Device, pos Position) error {

	var err error
	windowID := dev.ID
	log.Infof("show video for webcam %v", windowID)
	window := p.windows[windowID]
	if window == nil || !window.IsOpen() {
		return fmt.Errorf("windowID: %v not exist", windowID)
	}
	// todo 窗口固定最前
	window.Show()

	return err
}

// moveVideo 处理移动视频窗口请求
func (p *Player) moveVideo(windowID string, pos Position) error {
	var err error
	// 这里可以添加移动视频窗口的逻辑
	log.Infof("Moving video for webcam %d, Pos: %v", windowID, pos)
	window := p.windows[windowID]
	if window == nil || !window.IsOpen() {
		return fmt.Errorf("windowID: %v not exist", windowID)
	}
	window.MoveWindow(pos.x, pos.y)
	window.ResizeWindow(pos.width, pos.height)
	return err
}

// RetryFunc 尝试执行函数，最多重试 maxAttempts 次，每次间隔 interval 时间
func RetryFunc(fn func() error, maxAttempts int, interval time.Duration) error {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := fn(); err == nil {
			return nil // 函数执行成功，无需重试
		}

		log.Infof("Attempt %d failed. Retrying in %v...", attempt, interval)
		time.Sleep(interval)
	}

	return errors.New("Max attempts reached, function still failed")
}
