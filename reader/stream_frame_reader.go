package reader

// 本文件承载 Core 框架中与 `stream_frame_reader` 相关的通用逻辑。

import (
	"io"

	core "github.com/yttydcs/myflowhub-core"
)

// StreamFrameReader 是面向任意字节流承载的统一帧读取器。
type StreamFrameReader struct{}

var _ core.IFrameReader = (*StreamFrameReader)(nil)

// NewStreamFrameReader 创建一个不关心底层承载类型的帧读取器。
func NewStreamFrameReader() *StreamFrameReader {
	return &StreamFrameReader{}
}

// ReadFrame 直接委托 HeaderCodec 做解码，把 header 和 payload 组装成统一 Frame。
func (r *StreamFrameReader) ReadFrame(rd io.Reader, codec core.IHeaderCodec) (core.Frame, error) {
	hdr, payload, err := codec.Decode(rd)
	if err != nil {
		return core.Frame{}, err
	}
	return core.Frame{Header: hdr, Payload: payload}, nil
}
