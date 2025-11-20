package header

import (
	"encoding/binary"
	"io"
	"time"

	core "MyFlowHub-Core/internal/core"
)

// HeaderTcp 按方案A定义的 v1 头部，总长度 24 字节（由于 Source/Target 各 4 字节）：
// TypeFmt[1]；Flags[1]；MsgID[4]；Source[4]；Target[4]；Timestamp[4]；PayloadLen[4]；Reserved[2]
// 字段含义：
// - Flags：位域（如压缩、优先级、需回执等，v1 可按需使用）。
// - MsgID：uint32 会话内单调序列号（请求-响应关联、去重/重传窗口）。
// - Source/Target：uint32 全局节点 ID（v1 使用 4 字节）。
// - Timestamp：uint32 UTC 秒；0 表示未填。
// - PayloadLen：uint32 负载长度（字节）。
// - Reserved：uint16 保留为 0（对齐/未来扩展）。
type HeaderTcp struct {
	TypeFmt    uint8
	Flags      uint8
	MsgID      uint32
	Source     uint32
	Target     uint32
	Timestamp  uint32
	PayloadLen uint32
	Reserved   uint16
}

// 大类常量（TypeFmt bit0..1）
const (
	MajorOKResp  uint8 = 0
	MajorErrResp uint8 = 1
	MajorMsg     uint8 = 2
	MajorCmd     uint8 = 3
)

// Flags 预留位（可按需扩展）
const (
	FlagACKRequired uint8 = 1 << 0 // 需回执
	FlagCompressed  uint8 = 1 << 1 // 负载压缩
	// 其他位保留
)

// Major 返回消息大类（TypeFmt 的 bit0..1）。
func (h HeaderTcp) Major() uint8 { return h.TypeFmt & 0x03 }

// SubProto 返回子协议（TypeFmt 的 bit2..7）。
func (h HeaderTcp) SubProto() uint8 { return (h.TypeFmt >> 2) & 0x3F }

// Getter 适配 IHeader
func (h HeaderTcp) SourceID() uint32      { return h.Source }
func (h HeaderTcp) TargetID() uint32      { return h.Target }
func (h HeaderTcp) GetFlags() uint8       { return h.Flags }
func (h HeaderTcp) GetMsgID() uint32      { return h.MsgID }
func (h HeaderTcp) GetTimestamp() uint32  { return h.Timestamp }
func (h HeaderTcp) PayloadLength() uint32 { return h.PayloadLen }
func (h HeaderTcp) GetReserved() uint16   { return h.Reserved }

// WithMajor 设置消息大类（不会修改子协议位）。
func (h *HeaderTcp) WithMajor(major uint8) core.IHeader {
	h.TypeFmt = (h.TypeFmt &^ 0x03) | (major & 0x03)
	return h
}

// WithSubProto 设置子协议（不会修改大类位）。
func (h *HeaderTcp) WithSubProto(sub uint8) core.IHeader {
	h.TypeFmt = (h.TypeFmt &^ 0xFC) | ((sub & 0x3F) << 2)
	return h
}

func (h *HeaderTcp) WithSourceID(v uint32) core.IHeader      { h.Source = v; return h }
func (h *HeaderTcp) WithTargetID(v uint32) core.IHeader      { h.Target = v; return h }
func (h *HeaderTcp) WithFlags(v uint8) core.IHeader          { h.Flags = v; return h }
func (h *HeaderTcp) WithMsgID(v uint32) core.IHeader         { h.MsgID = v; return h }
func (h *HeaderTcp) WithTimestamp(v uint32) core.IHeader     { h.Timestamp = v; return h }
func (h *HeaderTcp) WithPayloadLength(v uint32) core.IHeader { h.PayloadLen = v; return h }
func (h *HeaderTcp) WithReserved(v uint16) core.IHeader      { h.Reserved = v; return h }

func (h *HeaderTcp) Clone() core.IHeader {
	if h == nil {
		return &HeaderTcp{}
	}
	clone := *h
	return &clone
}

// HeaderTcpCodec 提供 HeaderTcp 的编解码。
type HeaderTcpCodec struct{}

const headerTcpSize = 24

// Encode 将 HeaderTcp 与 payload 编码为 [header || payload]。
func (HeaderTcpCodec) Encode(header core.IHeader, payload []byte) ([]byte, error) {
	var h HeaderTcp
	if hp, ok := header.(*HeaderTcp); ok && hp != nil {
		h = *hp
	} else {
		// 从通用接口还原 TCP 头布局
		h = HeaderTcp{
			TypeFmt:    (header.Major() & 0x03) | ((header.SubProto() & 0x3F) << 2),
			Flags:      header.GetFlags(),
			MsgID:      header.GetMsgID(),
			Source:     header.SourceID(),
			Target:     header.TargetID(),
			Timestamp:  header.GetTimestamp(),
			PayloadLen: header.PayloadLength(),
			Reserved:   header.GetReserved(),
		}
	}
	if uint32(len(payload)) != h.PayloadLen {
		h.PayloadLen = uint32(len(payload))
	}

	buf := make([]byte, headerTcpSize+len(payload))
	buf[0] = h.TypeFmt
	buf[1] = h.Flags
	binary.BigEndian.PutUint32(buf[2:6], h.MsgID)
	binary.BigEndian.PutUint32(buf[6:10], h.Source)
	binary.BigEndian.PutUint32(buf[10:14], h.Target)
	binary.BigEndian.PutUint32(buf[14:18], h.Timestamp)
	binary.BigEndian.PutUint32(buf[18:22], h.PayloadLen)
	binary.BigEndian.PutUint16(buf[22:24], h.Reserved)
	copy(buf[headerTcpSize:], payload)
	return buf, nil
}

// Decode 从 reader 解码出一帧：先读 24 字节头，再按 PayloadLen 读取负载。
func (HeaderTcpCodec) Decode(r io.Reader) (core.IHeader, []byte, error) {
	hdr := make([]byte, headerTcpSize)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, nil, err
	}
	h := HeaderTcp{
		TypeFmt:    hdr[0],
		Flags:      hdr[1],
		MsgID:      binary.BigEndian.Uint32(hdr[2:6]),
		Source:     binary.BigEndian.Uint32(hdr[6:10]),
		Target:     binary.BigEndian.Uint32(hdr[10:14]),
		Timestamp:  binary.BigEndian.Uint32(hdr[14:18]),
		PayloadLen: binary.BigEndian.Uint32(hdr[18:22]),
		Reserved:   binary.BigEndian.Uint16(hdr[22:24]),
	}
	if h.PayloadLen == 0 {
		return &h, nil, nil
	}
	payload := make([]byte, h.PayloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, nil, err
	}
	return &h, payload, nil
}

func CloneToTCP(src core.IHeader) *HeaderTcp {
	if src == nil {
		return &HeaderTcp{}
	}
	if existing, ok := src.(*HeaderTcp); ok && existing != nil {
		clone := *existing
		return &clone
	}
	clone := &HeaderTcp{}
	clone.WithMajor(src.Major()).
		WithSubProto(src.SubProto()).
		WithSourceID(src.SourceID()).
		WithTargetID(src.TargetID()).
		WithFlags(src.GetFlags()).
		WithMsgID(src.GetMsgID()).
		WithTimestamp(src.GetTimestamp()).
		WithPayloadLength(src.PayloadLength()).
		WithReserved(src.GetReserved())
	return clone
}

func BuildTCPResponse(req core.IHeader, payloadLen uint32, sub uint8) *HeaderTcp {
	resp := CloneToTCP(req)
	resp.WithMajor(MajorOKResp).
		WithSubProto(sub).
		WithSourceID(req.TargetID()).
		WithTargetID(req.SourceID()).
		WithMsgID(req.GetMsgID()).
		WithTimestamp(uint32(time.Now().Unix())).
		WithPayloadLength(payloadLen)
	return resp
}
