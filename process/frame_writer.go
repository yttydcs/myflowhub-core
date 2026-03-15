package process

import (
	"encoding/binary"
	"io"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/header"
)

// StreamFrameWriter writes transport-neutral frames to byte streams.
type StreamFrameWriter struct{}

var _ core.IFrameWriter = (*StreamFrameWriter)(nil)

func NewStreamFrameWriter() *StreamFrameWriter {
	return &StreamFrameWriter{}
}

func (w *StreamFrameWriter) WriteFrame(dst io.Writer, codec core.IHeaderCodec, frame core.Frame) error {
	return WriteFrame(dst, codec, frame)
}

// WriteFrame encodes and writes a frame, preferring a zero-copy path for HeaderTcp.
func WriteFrame(dst io.Writer, codec core.IHeaderCodec, frame core.Frame) error {
	switch c := codec.(type) {
	case header.HeaderTcpCodec:
		return writeTCPFrame(dst, c, frame)
	case *header.HeaderTcpCodec:
		return writeTCPFrame(dst, *c, frame)
	default:
		encoded, err := codec.Encode(frame.Header, frame.Payload)
		if err != nil {
			return err
		}
		return core.WriteAll(dst, encoded)
	}
}

func writeTCPFrame(dst io.Writer, _ header.HeaderTcpCodec, frame core.Frame) error {
	tcpHdr := header.CloneToTCP(frame.Header)
	if tcpHdr == nil {
		return errNilCodec
	}
	if tcpHdr.HopLimit == 0 {
		tcpHdr.HopLimit = header.DefaultHopLimit
	}
	if uint32(len(frame.Payload)) != tcpHdr.PayloadLen {
		tcpHdr.PayloadLen = uint32(len(frame.Payload))
	}
	buf := make([]byte, 32)
	binary.BigEndian.PutUint16(buf[0:2], header.HeaderTcpMagicV2)
	buf[2] = header.HeaderTcpVersionV2
	buf[3] = 32
	buf[4] = tcpHdr.TypeFmt
	buf[5] = tcpHdr.Flags
	buf[6] = tcpHdr.HopLimit
	buf[7] = tcpHdr.RouteFlags
	binary.BigEndian.PutUint32(buf[8:12], tcpHdr.MsgID)
	binary.BigEndian.PutUint32(buf[12:16], tcpHdr.Source)
	binary.BigEndian.PutUint32(buf[16:20], tcpHdr.Target)
	binary.BigEndian.PutUint32(buf[20:24], tcpHdr.TraceID)
	binary.BigEndian.PutUint32(buf[24:28], tcpHdr.Timestamp)
	binary.BigEndian.PutUint32(buf[28:32], tcpHdr.PayloadLen)
	return core.WriteAllBuffers(dst, buf, frame.Payload)
}
