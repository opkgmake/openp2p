package openp2p

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"sync"
	"time"
)

type underlayTCP6 struct {
	writeMtx *sync.Mutex
	net.Conn
	reader            *bufio.Reader
	httpHeaderSent    bool
	httpHeaderSkipped bool
}

func (conn *underlayTCP6) Protocol() string {
	return "tcp6"
}

func (conn *underlayTCP6) ReadBuffer() (*openP2PHeader, []byte, error) {
	return DefaultReadBuffer(conn)
}

func (conn *underlayTCP6) WriteBytes(mainType uint16, subType uint16, data []byte) error {
	writeBytes := append(encodeHeader(mainType, subType, uint32(len(data))), data...)
	return conn.writeWithHTTPPrefix(writeBytes)
}

func (conn *underlayTCP6) WriteBuffer(data []byte) error {
	return conn.writeWithHTTPPrefix(data)
}

func (conn *underlayTCP6) WriteMessage(mainType uint16, subType uint16, packet interface{}) error {
	writeBytes, err := newMessage(mainType, subType, packet)
	if err != nil {
		return err
	}
	return conn.writeWithHTTPPrefix(writeBytes)
}

func (conn *underlayTCP6) Read(b []byte) (int, error) {
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

func (conn *underlayTCP6) Close() error {
	return conn.Conn.Close()
}
func (conn *underlayTCP6) WLock() {
	conn.writeMtx.Lock()
}
func (conn *underlayTCP6) WUnlock() {
	conn.writeMtx.Unlock()
}

func (conn *underlayTCP6) skipHTTPHeader() error {
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

func (conn *underlayTCP6) writeWithHTTPPrefix(data []byte) error {
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
func listenTCP6(port int, timeout time.Duration) (*underlayTCP6, error) {
	addr, _ := net.ResolveTCPAddr("tcp6", fmt.Sprintf("[::]:%d", port))
	l, err := net.ListenTCP("tcp6", addr)
	if err != nil {
		return nil, err
	}
	defer l.Close()
	l.SetDeadline(time.Now().Add(timeout))
	c, err := l.Accept()
	defer l.Close()
	if err != nil {
		return nil, err
	}
	return &underlayTCP6{writeMtx: &sync.Mutex{}, Conn: c, reader: bufio.NewReader(c)}, nil
}

func dialTCP6(host string, port int) (*underlayTCP6, error) {
	c, err := net.DialTimeout("tcp6", fmt.Sprintf("[%s]:%d", host, port), UnderlayConnectTimeout)
	if err != nil {
		gLog.Printf(LvERROR, "Dial %s:%d error:%s", host, port, err)
		return nil, err
	}
	return &underlayTCP6{writeMtx: &sync.Mutex{}, Conn: c, reader: bufio.NewReader(c)}, nil
}
