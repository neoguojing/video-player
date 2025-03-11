package rtsp

import (
	"errors"
	"fmt"
	"net"
	"time"
)

var ErrNoUDPPortPair = errors.New("no available udp port pairs")

// ErrRedirect error define
type ErrRedirect struct {
	URL string
}

func (e ErrRedirect) Error() string {
	return fmt.Sprintf("not the target rtsp server, should redirect ro (%s)", e.URL)
}

// FindUDPPair 返回一对RTP + RTCP链接
// udpPort:
//   =0 成功时使用系统随机分配的空闲端口
//   正偶数 成功时使用端口(udpPort, udpPort+1)
//   正奇数 成功时使用端口(udpPort-1, udpPort)
func FindUDPPair(udpPort uint32) []*net.UDPConn {
	return FindUDPPairEx("", udpPort)
}

// FindUDPPairEx 返回一对RTP + RTCP链接
// udpIP: 指定监听的IP
// udpPort:
//   =0 成功时使用系统随机分配的空闲端口
//   正偶数 成功时使用端口(udpPort, udpPort+1)
//   正奇数 成功时使用端口(udpPort-1, udpPort)
func FindUDPPairEx(udpIP string, udpPort uint32) []*net.UDPConn {

	for i := 0; i < 20; i++ {
		var c1, c2 *net.UDPConn
		var err error
		udpAddr1, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", udpIP, udpPort))
		if c1, err = net.ListenUDP("udp", udpAddr1); err != nil {
			return nil
		}

		p1 := c1.LocalAddr().(*net.UDPAddr).Port
		p2 := 0
		if p1&0x01 == 0 {
			p2 = p1 + 1
		} else {
			p2 = p1 - 1
		}

		udpAddr2, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", udpIP, p2))
		if c2, err = net.ListenUDP("udp", udpAddr2); err != nil {
			_ = c1.Close()
			if udpPort > 0 { // Retry 20 times when udpPort is equal to 0, otherwise do not retry
				return nil
			}

			time.Sleep(200 * time.Millisecond)
			continue
		}
		if p1 < p2 {
			return []*net.UDPConn{c1, c2}
		} else {
			return []*net.UDPConn{c2, c1}
		}
	}
	return nil
}
