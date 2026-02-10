package header

import (
	"encoding/binary"
	"errors"
	"io"
	"time"

	core "github.com/yttydcs/myflowhub-core"
)

// HeaderTcp 为 wire 头（v2），总长度 32 字节：
// Magic[2] Ver[1] HdrLen[1] TypeFmt[1] Flags[1] HopLimit[1] RouteFlags[1]
// MsgID[4] Source[4] Target[4] TraceID[4] Timestamp[4] PayloadLen[4]
//
// 说明：
// - Magic：用于快速判帧与防止错位读。
// - Ver/HdrLen：用于版本与扩展；当前 v2 固定 HdrLen=32，可向后追加字段（HdrLen>32 时 decoder 会读取并忽略扩展区）。
// - TypeFmt：bit0..1=Major；bit2..7=SubProto。
// - HopLimit：每发生一次“转发”递减 1，用于防环；0 视为未设置，按 DefaultHopLimit 处理。
// - RouteFlags：预留路由标志位（当前不定义语义）。
// - TraceID：跨 hop 关联日志与观测；0 视为未设置，可由发送链路自动填充。
type HeaderTcp struct {
	Magic      uint16
	Ver        uint8
	HdrLen     uint8
	TypeFmt    uint8
	Flags      uint8
	HopLimit   uint8
	RouteFlags uint8
	MsgID      uint32
	Source     uint32
	Target     uint32
	TraceID    uint32
	Timestamp  uint32
	PayloadLen uint32
}

// 大类常量（TypeFmt bit0..1）
const (
	MajorOKResp  uint8 = 0
	MajorErrResp uint8 = 1
	MajorMsg     uint8 = 2
	MajorCmd     uint8 = 3
)

const (
	HeaderTcpMagicV2   uint16 = 0x4D48 // "MH"
	HeaderTcpVersionV2 uint8  = 2
	DefaultHopLimit    uint8  = 16
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
func (h HeaderTcp) GetHopLimit() uint8    { return h.HopLimit }
func (h HeaderTcp) GetRouteFlags() uint8  { return h.RouteFlags }
func (h HeaderTcp) GetMsgID() uint32      { return h.MsgID }
func (h HeaderTcp) GetTraceID() uint32    { return h.TraceID }
func (h HeaderTcp) GetTimestamp() uint32  { return h.Timestamp }
func (h HeaderTcp) PayloadLength() uint32 { return h.PayloadLen }

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
func (h *HeaderTcp) WithHopLimit(v uint8) core.IHeader       { h.HopLimit = v; return h }
func (h *HeaderTcp) WithRouteFlags(v uint8) core.IHeader     { h.RouteFlags = v; return h }
func (h *HeaderTcp) WithMsgID(v uint32) core.IHeader         { h.MsgID = v; return h }
func (h *HeaderTcp) WithTraceID(v uint32) core.IHeader       { h.TraceID = v; return h }
func (h *HeaderTcp) WithTimestamp(v uint32) core.IHeader     { h.Timestamp = v; return h }
func (h *HeaderTcp) WithPayloadLength(v uint32) core.IHeader { h.PayloadLen = v; return h }

func (h *HeaderTcp) Clone() core.IHeader {
	if h == nil {
		return &HeaderTcp{}
	}
	clone := *h
	return &clone
}

// HeaderTcpCodec 提供 HeaderTcp 的编解码。
type HeaderTcpCodec struct{}

const headerTcpSize = 32

var (
	ErrHeaderMagicMismatch  = errors.New("header magic mismatch")
	ErrHeaderVersionInvalid = errors.New("header version invalid")
	ErrHeaderLenInvalid     = errors.New("header length invalid")
	ErrHeaderTooLarge       = errors.New("header too large")
)

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
			HopLimit:   header.GetHopLimit(),
			RouteFlags: header.GetRouteFlags(),
			MsgID:      header.GetMsgID(),
			Source:     header.SourceID(),
			Target:     header.TargetID(),
			TraceID:    header.GetTraceID(),
			Timestamp:  header.GetTimestamp(),
			PayloadLen: header.PayloadLength(),
		}
	}
	if h.HopLimit == 0 {
		h.HopLimit = DefaultHopLimit
	}
	if uint32(len(payload)) != h.PayloadLen {
		h.PayloadLen = uint32(len(payload))
	}
	h.Magic = HeaderTcpMagicV2
	h.Ver = HeaderTcpVersionV2
	h.HdrLen = headerTcpSize

	buf := make([]byte, headerTcpSize+len(payload))
	binary.BigEndian.PutUint16(buf[0:2], h.Magic)
	buf[2] = h.Ver
	buf[3] = h.HdrLen
	buf[4] = h.TypeFmt
	buf[5] = h.Flags
	buf[6] = h.HopLimit
	buf[7] = h.RouteFlags
	binary.BigEndian.PutUint32(buf[8:12], h.MsgID)
	binary.BigEndian.PutUint32(buf[12:16], h.Source)
	binary.BigEndian.PutUint32(buf[16:20], h.Target)
	binary.BigEndian.PutUint32(buf[20:24], h.TraceID)
	binary.BigEndian.PutUint32(buf[24:28], h.Timestamp)
	binary.BigEndian.PutUint32(buf[28:32], h.PayloadLen)
	copy(buf[headerTcpSize:], payload)
	return buf, nil
}

// Decode 从 reader 解码出一帧：先读头（最小 32B；允许 hdr_len>32 的扩展头），再按 PayloadLen 读取负载。
func (HeaderTcpCodec) Decode(r io.Reader) (core.IHeader, []byte, error) {
	prefix := make([]byte, 4)
	if _, err := io.ReadFull(r, prefix); err != nil {
		return nil, nil, err
	}
	magic := binary.BigEndian.Uint16(prefix[0:2])
	ver := prefix[2]
	hdrLen := prefix[3]
	if magic != HeaderTcpMagicV2 {
		return nil, nil, ErrHeaderMagicMismatch
	}
	if ver != HeaderTcpVersionV2 {
		return nil, nil, ErrHeaderVersionInvalid
	}
	if hdrLen < headerTcpSize {
		return nil, nil, ErrHeaderLenInvalid
	}
	// 防御：避免恶意 hdrLen 导致内存放大
	if hdrLen > 255 {
		return nil, nil, ErrHeaderTooLarge
	}
	hdr := make([]byte, hdrLen)
	copy(hdr[:4], prefix)
	if _, err := io.ReadFull(r, hdr[4:]); err != nil {
		return nil, nil, err
	}

	h := HeaderTcp{
		Magic:      magic,
		Ver:        ver,
		HdrLen:     hdrLen,
		TypeFmt:    hdr[4],
		Flags:      hdr[5],
		HopLimit:   hdr[6],
		RouteFlags: hdr[7],
		MsgID:      binary.BigEndian.Uint32(hdr[8:12]),
		Source:     binary.BigEndian.Uint32(hdr[12:16]),
		Target:     binary.BigEndian.Uint32(hdr[16:20]),
		TraceID:    binary.BigEndian.Uint32(hdr[20:24]),
		Timestamp:  binary.BigEndian.Uint32(hdr[24:28]),
		PayloadLen: binary.BigEndian.Uint32(hdr[28:32]),
	}
	if h.HopLimit == 0 {
		h.HopLimit = DefaultHopLimit
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
		WithHopLimit(src.GetHopLimit()).
		WithRouteFlags(src.GetRouteFlags()).
		WithMsgID(src.GetMsgID()).
		WithTraceID(src.GetTraceID()).
		WithTimestamp(src.GetTimestamp()).
		WithPayloadLength(src.PayloadLength())
	return clone
}

// CloneToTCPForForward 克隆头部并按 hop_limit 规则做一次递减（用于“转发”场景）。
// 返回 ok=false 表示 hop_limit 耗尽，建议丢弃该帧（避免环路/风暴）。
func CloneToTCPForForward(src core.IHeader) (*HeaderTcp, bool) {
	if src == nil {
		return nil, false
	}
	clone := CloneToTCP(src)
	hop := clone.HopLimit
	if hop == 0 {
		hop = DefaultHopLimit
	}
	if hop <= 1 {
		return nil, false
	}
	clone.HopLimit = hop - 1
	return clone, true
}

func BuildTCPResponse(req core.IHeader, payloadLen uint32, sub uint8) *HeaderTcp {
	resp := CloneToTCP(req)
	resp.WithMajor(MajorOKResp).
		WithSubProto(sub).
		WithSourceID(req.TargetID()).
		WithTargetID(req.SourceID()).
		WithMsgID(req.GetMsgID()).
		WithTraceID(req.GetTraceID()).
		WithHopLimit(DefaultHopLimit).
		WithTimestamp(uint32(time.Now().Unix())).
		WithPayloadLength(payloadLen)
	return resp
}
