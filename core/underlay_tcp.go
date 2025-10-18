package openp2p

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	reuse "github.com/openp2p-cn/go-reuseport"
)

type underlayTCP struct {
	writeMtx *sync.Mutex
	net.Conn
	connectTime       time.Time
	reader            *bufio.Reader
	httpHeaderSent    bool
	httpHeaderSkipped bool
}

func httpPreface() []byte {
	host := gConf.Network.HTTPHost
	if host == "" {
		host = "host"
	}
	return []byte(fmt.Sprintf("GET / HTTP/1.1\r\nHost: %s\r\n\r\n", host))
}

func (conn *underlayTCP) Protocol() string {
	return "tcp"
}

func (conn *underlayTCP) ReadBuffer() (*openP2PHeader, []byte, error) {
	return DefaultReadBuffer(conn)
}

func (conn *underlayTCP) WriteBytes(mainType uint16, subType uint16, data []byte) error {
	writeBytes := append(encodeHeader(mainType, subType, uint32(len(data))), data...)
	return conn.writeWithHTTPPrefix(writeBytes)
}

func (conn *underlayTCP) WriteBuffer(data []byte) error {
	return conn.writeWithHTTPPrefix(data)
}

func (conn *underlayTCP) WriteMessage(mainType uint16, subType uint16, packet interface{}) error {
	writeBytes, err := newMessage(mainType, subType, packet)
	if err != nil {
		return err
	}
	return conn.writeWithHTTPPrefix(writeBytes)
}

func (conn *underlayTCP) Read(b []byte) (int, error) {
	if conn.reader == nil {
		conn.reader = bufio.NewReader(conn.Conn)
	}
	if !conn.httpHeaderSkipped {
		if err := conn.skipHTTPHeader(); err != nil {
			return 0, err
		}
	}
	return conn.reader.Read(b)
}

func (conn *underlayTCP) Close() error {
	return conn.Conn.Close()
}
func (conn *underlayTCP) WLock() {
	conn.writeMtx.Lock()
}
func (conn *underlayTCP) WUnlock() {
	conn.writeMtx.Unlock()
}

func (conn *underlayTCP) skipHTTPHeader() error {
	if conn.reader == nil {
		conn.reader = bufio.NewReader(conn.Conn)
	}
	peek, err := conn.reader.Peek(4)
	if err != nil {
		return err
	}
	if len(peek) >= 4 && bytes.Equal(peek[:4], []byte("GET ")) {
		for {
			line, readErr := conn.reader.ReadString('\n')
			if readErr != nil {
				return readErr
			}
			if line == "\r\n" {
				break
			}
		}
	}
	conn.httpHeaderSkipped = true
	return nil
}

func (conn *underlayTCP) writeWithHTTPPrefix(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	conn.SetWriteDeadline(time.Now().Add(TunnelHeartbeatTime / 2))
	conn.WLock()
	defer conn.WUnlock()
	if !conn.httpHeaderSent {
		prefix := httpPreface()
		merged := make([]byte, len(prefix)+len(data))
		copy(merged, prefix)
		copy(merged[len(prefix):], data)
		data = merged
		conn.httpHeaderSent = true
	}
	_, err := conn.Conn.Write(data)
	return err
}

func listenTCP(host string, port int, localPort int, mode string, t *P2PTunnel) (*underlayTCP, error) {
	if mode == LinkModeTCPPunch {
		if compareVersion(t.config.peerVersion, SyncServerTimeVersion) < 0 {
			gLog.Printf(LvDEBUG, "peer version %s less than %s", t.config.peerVersion, SyncServerTimeVersion)
		} else {
			ts := time.Duration(int64(t.punchTs) + GNetwork.dt - time.Now().UnixNano())
			gLog.Printf(LvDEBUG, "sleep %d ms", ts/time.Millisecond)
			time.Sleep(ts)
		}
		gLog.Println(LvDEBUG, " send tcp punch: ", fmt.Sprintf("0.0.0.0:%d", localPort), "-->", fmt.Sprintf("%s:%d", host, port))
		c, err := reuse.DialTimeout("tcp", fmt.Sprintf("0.0.0.0:%d", localPort), fmt.Sprintf("%s:%d", host, port), CheckActiveTimeout)
		if err != nil {
			gLog.Println(LvDEBUG, "send tcp punch: ", err)
			return nil, err
		}
		utcp := &underlayTCP{writeMtx: &sync.Mutex{}, Conn: c, reader: bufio.NewReader(c)}
		_, buff, err := utcp.ReadBuffer()
		if err != nil {
			return nil, fmt.Errorf("read start msg error:%s", err)
		}
		if buff != nil {
			gLog.Println(LvDEBUG, string(buff))
		}
		utcp.WriteBytes(MsgP2P, MsgTunnelHandshakeAck, buff)
		return utcp, nil
	}
	GNetwork.push(t.config.PeerNode, MsgPushUnderlayConnect, nil)
	tid := t.id
	if compareVersion(t.config.peerVersion, PublicIPVersion) < 0 { // old version
		ipBytes := net.ParseIP(t.config.peerIP).To4()
		tid = uint64(binary.BigEndian.Uint32(ipBytes))
		gLog.Println(LvDEBUG, "compatible with old client, use ip as key:", tid)
	}
	var utcp *underlayTCP
	if mode == LinkModeIntranet && gConf.Network.hasIPv4 == 0 && gConf.Network.hasUPNPorNATPMP == 0 {
		addr, _ := net.ResolveTCPAddr("tcp4", fmt.Sprintf("0.0.0.0:%d", localPort))
		l, err := net.ListenTCP("tcp4", addr)
		if err != nil {
			gLog.Printf(LvERROR, "listen %d error:", localPort, err)
			return nil, err
		}
		defer l.Close()
		err = l.SetDeadline(time.Now().Add(UnderlayTCPConnectTimeout))
		if err != nil {
			gLog.Printf(LvERROR, "set listen timeout:", err)
			return nil, err
		}
		c, err := l.Accept()
		if err != nil {
			return nil, err
		}
		utcp = &underlayTCP{writeMtx: &sync.Mutex{}, Conn: c, reader: bufio.NewReader(c)}
	} else {
		if v4l != nil {
			utcp = v4l.getUnderlayTCP(tid)
		}
	}

	if utcp == nil {
		return nil, ErrConnectPublicV4
	}
	return utcp, nil
}

func dialTCP(host string, port int, localPort int, mode string) (*underlayTCP, error) {
	var c net.Conn
	var err error
	if mode == LinkModeTCPPunch {
		gLog.Println(LvDev, " send tcp punch: ", fmt.Sprintf("0.0.0.0:%d", localPort), "-->", fmt.Sprintf("%s:%d", host, port))
		if c, err = reuse.DialTimeout("tcp", fmt.Sprintf("0.0.0.0:%d", localPort), fmt.Sprintf("%s:%d", host, port), CheckActiveTimeout); err != nil {
			gLog.Println(LvDev, "send tcp punch: ", err)
		}

	} else {
		c, err = net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), CheckActiveTimeout)
	}

	if err != nil {
		gLog.Printf(LvDev, "Dial %s:%d error:%s", host, port, err)
		return nil, err
	}
	tc := c.(*net.TCPConn)
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(UnderlayTCPKeepalive)
	gLog.Printf(LvDEBUG, "Dial %s:%d OK", host, port)
	return &underlayTCP{writeMtx: &sync.Mutex{}, Conn: c, reader: bufio.NewReader(c)}, nil
}
