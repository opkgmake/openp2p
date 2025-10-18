package openp2p

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

var ErrDeadlineExceeded error = &DeadlineExceededError{}

const rawReadyTimeout = time.Second * 5

// DeadlineExceededError is returned for an expired deadline.
type DeadlineExceededError struct{}

// Implement the net.Error interface.
// The string is "i/o timeout" because that is what was returned
// by earlier Go versions. Changing it may break programs that
// match on error strings.
func (e *DeadlineExceededError) Error() string   { return "i/o timeout" }
func (e *DeadlineExceededError) Timeout() bool   { return true }
func (e *DeadlineExceededError) Temporary() bool { return true }

// implement io.Writer
type overlayConn struct {
	tunnel      *P2PTunnel // TODO: del
	app         *p2pApp
	connTCP     net.Conn
	id          uint64
	rtid        uint64
	running     bool
	isClient    bool
	appID       uint64 // TODO: del
	appKey      uint64 // TODO: del
	appKeyBytes []byte // TODO: del
	// for udp
	connUDP       *net.UDPConn
	remoteAddr    net.Addr
	udpData       chan []byte
	lastReadUDPTs time.Time
}

func (oConn *overlayConn) run() {
	if oConn.tunnel.useRawDirect() && oConn.connTCP != nil && oConn.rtid == 0 {
		if oConn.prepareRawSession() {
			oConn.runRaw()
			return
		}
	}
	oConn.runFramed()
}

func (oConn *overlayConn) prepareRawSession() bool {
	if !oConn.tunnel.beginRawSession() {
		gLog.Printf(LvERROR, "%d overlayConn raw session already active", oConn.id)
		oConn.tunnel.overlayConns.Delete(oConn.id)
		req := OverlayDisconnectReq{ID: oConn.id, AppID: oConn.appID}
		if err := oConn.tunnel.sendOverlayDisconnect(oConn.rtid, &req); err != nil {
			gLog.Printf(LvERROR, "overlayConn %d send disconnect error:%s", oConn.id, err)
		}
		oConn.Close()
		return false
	}
	if oConn.isClient {
		if !oConn.tunnel.awaitRawReady(oConn.id, rawReadyTimeout) {
			gLog.Printf(LvERROR, "%d overlayConn raw handshake timeout", oConn.id)
			oConn.tunnel.rawDirect = false
			oConn.tunnel.startFramedLoops()
			oConn.tunnel.endRawSession()
			oConn.tunnel.clearRawReady(oConn.id)
			return false
		}
	}
	return true
}

func (oConn *overlayConn) runFramed() {
	gLog.Printf(LvDEBUG, "%d overlayConn run start", oConn.id)
	defer gLog.Printf(LvDEBUG, "%d overlayConn run end", oConn.id)
	defer oConn.tunnel.clearRawReady(oConn.id)
	oConn.lastReadUDPTs = time.Now()
	buffer := make([]byte, ReadBuffLen+PaddingSize) // 16 bytes for padding
	reuseBuff := buffer[:ReadBuffLen]
	encryptData := make([]byte, ReadBuffLen+PaddingSize) // 16 bytes for padding
	tunnelHead := new(bytes.Buffer)
	relayHead := new(bytes.Buffer)
	binary.Write(relayHead, binary.LittleEndian, oConn.rtid)
	binary.Write(tunnelHead, binary.LittleEndian, oConn.id)
	for oConn.running && oConn.tunnel.isRuning() {
		readBuff, dataLen, err := oConn.Read(reuseBuff)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			// overlay tcp connection normal close, debug log
			gLog.Printf(LvDEBUG, "overlayConn %d read error:%s,close it", oConn.id, err)
			break
		}
		payload := readBuff[:dataLen]
		if oConn.appKey != 0 {
			payload, _ = encryptBytes(oConn.appKeyBytes, encryptData, readBuff[:dataLen], dataLen)
		}
		writeBytes := append(tunnelHead.Bytes(), payload...)
		// TODO: app.write
		if oConn.rtid == 0 {
			oConn.tunnel.conn.WriteBytes(MsgP2P, MsgOverlayData, writeBytes)
			gLog.Printf(LvDev, "write overlay data to tid:%d,oid:%d bodylen=%d", oConn.tunnel.id, oConn.id, len(writeBytes))
		} else {
			// write raley data
			all := append(relayHead.Bytes(), encodeHeader(MsgP2P, MsgOverlayData, uint32(len(writeBytes)))...)
			all = append(all, writeBytes...)
			oConn.tunnel.conn.WriteBytes(MsgP2P, MsgRelayData, all)
			gLog.Printf(LvDev, "write relay data to tid:%d,rtid:%d,oid:%d bodylen=%d", oConn.tunnel.id, oConn.rtid, oConn.id, len(writeBytes))
		}
	}
	if oConn.connTCP != nil {
		oConn.connTCP.Close()
	}
	if oConn.connUDP != nil {
		oConn.connUDP.Close()
	}
	oConn.tunnel.overlayConns.Delete(oConn.id)
	// notify peer disconnect
	req := OverlayDisconnectReq{ID: oConn.id, AppID: oConn.appID}
	if err := oConn.tunnel.sendOverlayDisconnect(oConn.rtid, &req); err != nil {
		gLog.Printf(LvERROR, "overlayConn %d send disconnect error:%s", oConn.id, err)
	}
}

func (oConn *overlayConn) runRaw() {
	gLog.Printf(LvDEBUG, "%d overlayConn raw run start", oConn.id)
	defer gLog.Printf(LvDEBUG, "%d overlayConn raw run end", oConn.id)
	defer oConn.tunnel.clearRawReady(oConn.id)
	if oConn.connTCP == nil {
		gLog.Printf(LvERROR, "%d overlayConn raw mode requires tcp connection", oConn.id)
		return
	}
	defer oConn.tunnel.endRawSession()
	done := make(chan struct{}, 2)
	var closeOnce sync.Once
	closeAll := func() {
		closeOnce.Do(func() {
			if oConn.connTCP != nil {
				oConn.connTCP.Close()
			}
			if oConn.tunnel.conn != nil {
				oConn.tunnel.conn.Close()
			}
		})
	}
	go func() {
		if oConn.tunnel.conn != nil {
			if _, err := io.Copy(oConn.tunnel.conn, oConn.connTCP); err != nil && !errors.Is(err, io.EOF) {
				gLog.Printf(LvDEBUG, "%d overlayConn raw upstream error:%s", oConn.id, err)
			}
		}
		done <- struct{}{}
	}()
	go func() {
		if oConn.tunnel.conn != nil {
			if _, err := io.Copy(oConn.connTCP, oConn.tunnel.conn); err != nil && !errors.Is(err, io.EOF) {
				gLog.Printf(LvDEBUG, "%d overlayConn raw downstream error:%s", oConn.id, err)
			}
		}
		done <- struct{}{}
	}()
	<-done
	closeAll()
	<-done
	oConn.tunnel.overlayConns.Delete(oConn.id)
	req := OverlayDisconnectReq{ID: oConn.id, AppID: oConn.appID}
	if err := oConn.tunnel.sendOverlayDisconnect(oConn.rtid, &req); err != nil {
		gLog.Printf(LvERROR, "overlayConn %d send disconnect error:%s", oConn.id, err)
	}
	oConn.tunnel.close()
}

func (oConn *overlayConn) Read(reuseBuff []byte) (buff []byte, dataLen int, err error) {
	if !oConn.running {
		err = ErrOverlayConnDisconnect
		return
	}
	if oConn.connUDP != nil {
		if time.Now().After(oConn.lastReadUDPTs.Add(time.Minute * 5)) {
			err = errors.New("udp close")
			return
		}
		if oConn.remoteAddr != nil { // as server
			select {
			case buff = <-oConn.udpData:
				dataLen = len(buff) - PaddingSize
				oConn.lastReadUDPTs = time.Now()
			case <-time.After(time.Second * 10):
				err = ErrDeadlineExceeded
			}
		} else { // as client
			oConn.connUDP.SetReadDeadline(time.Now().Add(UDPReadTimeout))
			dataLen, _, err = oConn.connUDP.ReadFrom(reuseBuff)
			if err == nil {
				oConn.lastReadUDPTs = time.Now()
			}
			buff = reuseBuff
		}
		return
	}
	if oConn.connTCP != nil {
		oConn.connTCP.SetReadDeadline(time.Now().Add(UDPReadTimeout))
		dataLen, err = oConn.connTCP.Read(reuseBuff)
		buff = reuseBuff
	}

	return
}

// calling by p2pTunnel
func (oConn *overlayConn) Write(buff []byte) (n int, err error) {
	// add mutex when multi-thread calling
	if !oConn.running {
		return 0, ErrOverlayConnDisconnect
	}
	if oConn.connUDP != nil {
		if oConn.remoteAddr == nil {
			n, err = oConn.connUDP.Write(buff)
		} else {
			n, err = oConn.connUDP.WriteTo(buff, oConn.remoteAddr)
		}
		if err != nil {
			oConn.running = false
		}
		return
	}
	if oConn.connTCP != nil {
		n, err = oConn.connTCP.Write(buff)
	}

	if err != nil {
		oConn.running = false
	}
	return
}

func (oConn *overlayConn) Close() (err error) {
	oConn.running = false
	if oConn.connTCP != nil {
		oConn.connTCP.Close()
		// oConn.connTCP = nil
	}
	if oConn.connUDP != nil {
		oConn.connUDP.Close()
		// oConn.connUDP = nil
	}
	return nil
}
