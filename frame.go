package core

import "io"

// Frame is the transport-neutral in-memory frame representation.
//
// Router / forward paths should prefer operating on Frame and keep Payload as raw
// bytes unless a local protocol handler explicitly needs to decode it.
type Frame struct {
	Header  IHeader
	Payload []byte
}

// IFrameReader extracts one frame from a byte stream.
type IFrameReader interface {
	ReadFrame(r io.Reader, codec IHeaderCodec) (Frame, error)
}

// IFrameWriter encodes and writes one frame to a byte stream.
type IFrameWriter interface {
	WriteFrame(w io.Writer, codec IHeaderCodec, frame Frame) error
}
