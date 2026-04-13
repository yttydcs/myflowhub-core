package reader

// Context: This file provides shared Core framework logic around stream_frame_reader.

import (
	"io"

	core "github.com/yttydcs/myflowhub-core"
)

// StreamFrameReader is the transport-neutral frame reader for byte streams.
type StreamFrameReader struct{}

var _ core.IFrameReader = (*StreamFrameReader)(nil)

func NewStreamFrameReader() *StreamFrameReader {
	return &StreamFrameReader{}
}

func (r *StreamFrameReader) ReadFrame(rd io.Reader, codec core.IHeaderCodec) (core.Frame, error) {
	hdr, payload, err := codec.Decode(rd)
	if err != nil {
		return core.Frame{}, err
	}
	return core.Frame{Header: hdr, Payload: payload}, nil
}
