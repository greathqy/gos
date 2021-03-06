package utils

import (
	"goslib/logger"
	"goslib/packet"
	"net"
)

type Buffer struct {
	ctrl    chan bool   // receive exit signal
	pending chan []byte // pending Packet
	max     int         // max queue size
	conn    net.Conn    // connection
}

func (buf *Buffer) Send(data []byte) (err error) {
	defer func() {
		if x := recover(); x != nil {
			logger.WARN("buffer.Send failed", x)
		}
	}()

	buf.pending <- data
	return nil
}

func (buf *Buffer) Start() {
	defer func() {
		if x := recover(); x != nil {
			logger.ERR("caught panic in buffer goroutine", x)
		}
	}()

	for {
		select {
		case data := <-buf.pending:
			buf.raw_send(data)
		case <-buf.ctrl:
			close(buf.pending)
			for data := range buf.pending {
				buf.raw_send(data)
			}
			buf.conn.Close()
			return
		}
	}
}

func (buf *Buffer) raw_send(data []byte) {
	writer := packet.Writer()
	writer.WriteUint16(uint16(len(data)))
	writer.WriteRawBytes(data)

	n, err := buf.conn.Write(writer.Data())
	if err != nil {
		logger.ERR("Error send reply, bytes:", n, "reason:", err)
		return
	}
}

func NewBuffer(conn net.Conn, ctrl chan bool, max int) *Buffer {
	buf := Buffer{conn: conn}
	buf.pending = make(chan []byte, max)
	buf.ctrl = ctrl
	buf.max = max
	return &buf
}
