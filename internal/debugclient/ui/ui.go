package ui

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/header"
	"MyFlowHub-Core/internal/debugclient/session"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

type Controller struct {
	app     fyne.App
	ctx     context.Context
	session *session.Session
	logBuf  bytes.Buffer
	logMu   sync.Mutex

	addrEntry *widget.Entry
	payload   *widget.Entry
	logView   *logEntry
	form      *headerForm

	nodeEntry *widget.Entry
	hexToggle *widget.Check
}

func New(app fyne.App, ctx context.Context) *Controller {
	c := &Controller{app: app, ctx: ctx}
	c.session = session.New(c.ctx, c.handleFrame, c.handleError)
	return c
}

func (c *Controller) Build(w fyne.Window) fyne.CanvasObject {
	c.addrEntry = widget.NewEntry()
	c.addrEntry.SetPlaceHolder("127.0.0.1:9000")

	c.nodeEntry = widget.NewEntry()
	c.nodeEntry.SetPlaceHolder("debugclient")

	connectBtn := widget.NewButton("连接", func() {
		addr := strings.TrimSpace(c.addrEntry.Text)
		if addr == "" {
			dialog.ShowError(fmt.Errorf("请填写地址"), w)
			return
		}
		go func() {
			if err := c.session.Connect(addr); err != nil {
				c.appendLog("[ERR] connect: %v", err)
				return
			}
			if err := c.session.Login(strings.TrimSpace(c.nodeEntry.Text)); err != nil {
				c.appendLog("[ERR] login: %v", err)
			} else {
				c.appendLog("[OK] connected %s", addr)
			}
		}()
	})

	disconnectBtn := widget.NewButton("断开", func() {
		c.session.Close()
		c.appendLog("[INFO] 手动断开")
	})

	headerCard := c.buildHeaderCard()
	payloadCard := c.buildPayloadCard()
	logCard := c.buildLogCard()

	sendBtn := widget.NewButton("发送", func() {
		hdr, err := c.form.Parse()
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		payload, perr := c.buildPayload()
		if perr != nil {
			dialog.ShowError(perr, w)
			return
		}
		if err := c.session.Send(hdr, payload); err != nil {
			c.appendLog("[ERR] send: %v", err)
			return
		}
		c.appendLog("[TX] sub=%d len=%d", hdr.SubProto(), len(payload))
	})

	topBar := container.NewBorder(nil, nil,
		widget.NewLabel("Server"),
		container.NewHBox(connectBtn, disconnectBtn),
		container.NewMax(c.addrEntry))

	content := container.NewVSplit(
		container.NewVBox(headerCard, payloadCard),
		logCard,
	)
	content.SetOffset(0.55)

	return container.NewBorder(topBar, sendBtn, nil, nil, content)
}

func (c *Controller) Shutdown() { c.session.Close() }

func (c *Controller) handleFrame(h core.IHeader, payload []byte) {
	preview := formatPayloadPreview(payload)
	c.appendLog("[RX] major=%d sub=%d src=%d tgt=%d len=%d %s",
		h.Major(), h.SubProto(), h.SourceID(), h.TargetID(), len(payload), preview)
}

func (c *Controller) handleError(err error) {
	c.appendLog("[ERR] %v", err)
}

func (c *Controller) appendLog(format string, args ...any) {
	c.logMu.Lock()
	defer c.logMu.Unlock()
	c.logBuf.WriteString(time.Now().Format("15:04:05 "))
	c.logBuf.WriteString(fmt.Sprintf(format, args...))
	c.logBuf.WriteString("\n")
	text := c.logBuf.String()
	if c.logView != nil {
		c.logView.SetText(text)
		c.logView.CursorRow = strings.Count(text, "\n")
	}
}

// UI helpers
func (c *Controller) buildHeaderCard() fyne.CanvasObject {
	c.form = newHeaderForm()
	return widget.NewCard("Header", "编辑 TCP 头字段", c.form.container)
}

func (c *Controller) buildPayloadCard() fyne.CanvasObject {
	c.payload = widget.NewMultiLineEntry()
	c.payload.SetPlaceHolder("请输入 payload 文本")
	c.hexToggle = widget.NewCheck("使用十六进制输入", nil)
	return widget.NewCard("Payload", "", container.NewBorder(nil, c.hexToggle, nil, nil, c.payload))
}

func (c *Controller) buildLogCard() fyne.CanvasObject {
	c.logView = newLogEntry()
	return widget.NewCard("日志", "", c.logView)
}

// header form parsing
type headerForm struct {
	container fyne.CanvasObject
	major     *widget.Entry
	sub       *widget.Entry
	source    *widget.Entry
	target    *widget.Entry
	msgID     *widget.Entry
	flags     *widget.Entry
}

func newHeaderForm() *headerForm {
	form := &headerForm{
		major:  widget.NewEntry(),
		sub:    widget.NewEntry(),
		source: widget.NewEntry(),
		target: widget.NewEntry(),
		msgID:  widget.NewEntry(),
		flags:  widget.NewEntry(),
	}
	form.major.SetText("2")
	form.sub.SetText("1")
	form.source.SetText("1")
	form.msgID.SetText("1")
	form.container = widget.NewForm(
		widget.NewFormItem("Major", form.major),
		widget.NewFormItem("SubProto", form.sub),
		widget.NewFormItem("SourceID", form.source),
		widget.NewFormItem("TargetID", form.target),
		widget.NewFormItem("MsgID", form.msgID),
		widget.NewFormItem("Flags", form.flags),
	)
	return form
}

func (f *headerForm) Parse() (core.IHeader, error) {
	major, err := parseUint("Major", f.major.Text, 0, 3)
	if err != nil {
		return nil, err
	}
	sub, err := parseUint("SubProto", f.sub.Text, 0, 63)
	if err != nil {
		return nil, err
	}
	src, err := parseUint("SourceID", f.source.Text, 0, 1<<32-1)
	if err != nil {
		return nil, err
	}
	tgt, err := parseUint("TargetID", f.target.Text, 0, 1<<32-1)
	if err != nil {
		return nil, err
	}
	msgID, err := parseUint("MsgID", f.msgID.Text, 0, 1<<32-1)
	if err != nil {
		return nil, err
	}
	flags, err := parseUint("Flags", f.flags.Text, 0, 255)
	if err != nil {
		return nil, err
	}

	hdr := &header.HeaderTcp{}
	hdr.WithMajor(uint8(major)).
		WithSubProto(uint8(sub)).
		WithSourceID(uint32(src)).
		WithTargetID(uint32(tgt)).
		WithMsgID(uint32(msgID)).
		WithFlags(uint8(flags)).
		WithTimestamp(uint32(time.Now().Unix()))
	return hdr, nil
}

func parseUint(field, text string, min, max uint64) (uint64, error) {
	if strings.TrimSpace(text) == "" {
		return 0, fmt.Errorf("%s 不能为空", field)
	}
	v, err := strconv.ParseUint(strings.TrimSpace(text), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s 不是合法整数", field)
	}
	if v < min || v > max {
		return 0, fmt.Errorf("%s 超出范围[%d,%d]", field, min, max)
	}
	return v, nil
}

func (c *Controller) buildPayload() ([]byte, error) {
	if !c.hexToggle.Checked {
		return []byte(c.payload.Text), nil
	}
	cleaned := strings.ReplaceAll(strings.TrimSpace(c.payload.Text), " ", "")
	if cleaned == "" {
		return nil, nil
	}
	data, err := hex.DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("十六进制解析失败: %w", err)
	}
	return data, nil
}

const (
	payloadPreviewLimit  = 256
	payloadTextRuneLimit = 128
)

func formatPayloadPreview(payload []byte) string {
	if len(payload) == 0 {
		return "payload=<empty>"
	}

	truncated := len(payload) > payloadPreviewLimit
	preview := payload
	if truncated {
		preview = payload[:payloadPreviewLimit]
	}

	textPart := buildTextPreview(preview)
	hexPart := bytesToSpacedHex(preview)

	suffix := ""
	if truncated {
		suffix = fmt.Sprintf("...(总长 %d bytes)", len(payload))
	}
	return fmt.Sprintf("payload=text(%s)%s hex=%s%s", textPart, suffix, hexPart,
		ternarySuffix(truncated))
}

func buildTextPreview(data []byte) string {
	if utf8.Valid(data) {
		runes := []rune(string(data))
		if len(runes) > payloadTextRuneLimit {
			return string(runes[:payloadTextRuneLimit]) + "..."
		}
		return string(runes)
	}
	var b strings.Builder
	for _, bt := range data {
		if bt >= 32 && bt <= 126 {
			b.WriteByte(bt)
			continue
		}
		b.WriteByte('.')
	}
	return b.String()
}

func bytesToSpacedHex(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	var b strings.Builder
	for i, bt := range data {
		if i > 0 && i%2 == 0 {
			b.WriteByte(' ')
		}
		b.WriteString(strings.ToUpper(fmt.Sprintf("%02x", bt)))
	}
	return b.String()
}

func ternarySuffix(cond bool) string {
	if cond {
		return " (截断)"
	}
	return ""
}

type logEntry struct {
	widget.Entry
}

func newLogEntry() *logEntry {
	e := &logEntry{}
	e.MultiLine = true
	e.Wrapping = fyne.TextWrapWord
	e.ExtendBaseWidget(e)
	return e
}

func (e *logEntry) TypedRune(r rune) {}

func (e *logEntry) TypedKey(event *fyne.KeyEvent) {
	switch event.Name {
	case fyne.KeyLeft, fyne.KeyRight, fyne.KeyUp, fyne.KeyDown,
		fyne.KeyHome, fyne.KeyEnd, fyne.KeyPageUp, fyne.KeyPageDown:
		e.Entry.TypedKey(event)
	}
}

func (e *logEntry) TypedShortcut(shortcut fyne.Shortcut) {
	switch shortcut.(type) {
	case *fyne.ShortcutCopy, *fyne.ShortcutSelectAll, *fyne.ShortcutPaste:
		// 允许复制/全选/粘贴（粘贴只改变剪贴板，不写入本区域）。
		e.Entry.TypedShortcut(shortcut)
	}
}
