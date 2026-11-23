package imediag

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

const (
	RingCap         = 256
	MaxMessageBytes = 65536
	messagePrefix   = "ime-diag:"
)

var nativeKinds = []string{"active", "inactive", "set-focus", "kill-focus", "move", "resize", "dpi", "show", "hide"}

var (
	errNotArmed = errors.New("尚未開始匯出")
	errSchema   = errors.New("格式不符")
	errRate     = errors.New("過於頻繁")
	errOrigin   = errors.New("來源不符")
	errWindow   = errors.New("視窗不符")
	errSize     = errors.New("訊息過大")
	nowFn       = time.Now
)

type Geo struct {
	Known bool
	X, Y  int
	W, H  int
}

type NativeEvent struct {
	T    int64  `json:"t"`
	Kind string `json:"kind"`
	X    *int   `json:"x,omitempty"`
	Y    *int   `json:"y,omitempty"`
	W    *int   `json:"w,omitempty"`
	H    *int   `json:"h,omitempty"`
	DPI  string `json:"dpi,omitempty"`
}

type Inbound struct {
	WindowName string
	Origin     string
	TopOrigin  string
	Home       string
	Body       string
}

type Meta struct {
	AppVersion      string
	WebView2Runtime string
}

type Log struct {
	mu         sync.Mutex
	native     []NativeEvent
	after      []NativeEvent
	frozen     bool
	busy       bool
	armed      bool
	dom        json.RawMessage
	domOK      bool
	lastAccept time.Time
}

func New() *Log { return &Log{} }

func (l *Log) AddNative(kind string, geo Geo) {
	if !nativeKind(kind) {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	ev := NativeEvent{T: nowFn().UnixMilli(), Kind: kind}
	if kind == "dpi" {
		ev.DPI = "unknown"
	}
	if geo.Known {
		ev.X, ev.Y, ev.W, ev.H = ptr(geo.X), ptr(geo.Y), ptr(geo.W), ptr(geo.H)
	}
	l.native = append(l.native, ev)
	if len(l.native) > RingCap {
		l.native = append([]NativeEvent(nil), l.native[len(l.native)-RingCap:]...)
	}
	if l.frozen {
		l.after = append(l.after, ev)
		if len(l.after) > RingCap {
			l.after = append([]NativeEvent(nil), l.after[len(l.after)-RingCap:]...)
		}
	}
}

// BeginExport 在一次匯出的等待與儲存對話框期間擋住再入。第二次呼叫不清除已凍結的快照。
func (l *Log) BeginExport() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.busy {
		return false
	}
	l.busy = true
	return true
}

// EndExport 結束這次匯出：停止 after 收集並放掉忙碌，避免視窗事件一直堆在額外 slice。
func (l *Log) EndExport() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.busy = false
	l.frozen = false
	l.armed = false
	l.after = nil
}

func (l *Log) FreezeNative() []NativeEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.frozen = true
	l.after = nil
	return append([]NativeEvent(nil), l.native...)
}

func (l *Log) NativeAfter() []NativeEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]NativeEvent(nil), l.after...)
}

func (l *Log) Arm() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.armed = true
	l.dom = nil
	l.domOK = false
}

func (l *Log) DOM() (json.RawMessage, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.domOK {
		return nil, false
	}
	return append(json.RawMessage(nil), l.dom...), true
}

func (l *Log) WaitDOM(d time.Duration) (json.RawMessage, bool) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	tick := time.NewTicker(15 * time.Millisecond)
	defer tick.Stop()
	for {
		if raw, ok := l.DOM(); ok {
			l.disarm()
			return raw, true
		}
		select {
		case <-timer.C:
			l.disarm()
			return nil, false
		case <-tick.C:
		}
	}
}

func (l *Log) Accept(in Inbound) error {
	if len(in.Body) > MaxMessageBytes {
		return errSize
	}
	if !strings.HasPrefix(in.Body, messagePrefix) {
		return errSchema
	}
	if in.WindowName != "main" {
		return errWindow
	}
	// Windows 上這版 Wails 沒有可靠的 IsMainFrame。這裡只比對首頁 origin，不能當成原生 main frame 已驗證。
	if in.Home == "" || in.Origin != in.Home || in.TopOrigin != in.Home {
		return errOrigin
	}
	clean, err := cleanSnapshot(strings.TrimPrefix(in.Body, messagePrefix))
	if err != nil {
		return errSchema
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.armed {
		return errNotArmed
	}
	if delta := nowFn().Sub(l.lastAccept); !l.lastAccept.IsZero() && delta >= 0 && delta < time.Second {
		return errRate
	}
	l.dom = clean
	l.domOK = true
	l.lastAccept = nowFn()
	return nil
}

func (l *Log) disarm() {
	l.mu.Lock()
	l.armed = false
	l.mu.Unlock()
}

func IsMessage(msg string) bool { return strings.HasPrefix(msg, messagePrefix) }

func Build(dom json.RawMessage, domOK bool, native []NativeEvent, after []NativeEvent, meta Meta) ([]byte, error) {
	doc := exportDoc{
		Kind:                   "ime-diagnostic",
		AppVersion:             unknownIfEmpty(meta.AppVersion),
		WebView2Runtime:        unknownIfEmpty(meta.WebView2Runtime),
		IMEMode:                "unknown",
		NativeFocusMeaning:     "window-events-only",
		FrozenBeforeExportUI:   true,
		MenuMayPrecedeSnapshot: true,
		Note:                   "選單可能在按下匯出前就讓視窗失焦，那段會留在快照。DOM 與 native.events 都在儲存對話框之前凍結；對話框之後的 focus 不在 native.events，不能解讀成異常已恢復。imeMode 固定 unknown。這是診斷，不是修復。",
	}
	doc.Native.Events = native
	doc.AfterExportUI.Native = after
	if domOK {
		events, truncated, err := cleanDOM(dom)
		if err != nil {
			return nil, errSchema
		}
		doc.DOM.Available = true
		doc.DOM.Truncated = truncated
		doc.DOM.Events = events
	}
	if doc.Native.Events == nil {
		doc.Native.Events = []NativeEvent{}
	}
	if doc.AfterExportUI.Native == nil {
		doc.AfterExportUI.Native = []NativeEvent{}
	}
	return json.Marshal(doc)
}

func Commit(data []byte, choose func() (string, error), write func(string, []byte) error) (bool, error) {
	path, err := choose()
	path, err = NormalizeChoice(path, err)
	if err != nil {
		return false, err
	}
	if path == "" {
		return false, nil
	}
	if err := write(path, data); err != nil {
		return false, err
	}
	return true, nil
}

func NormalizeChoice(path string, err error) (string, error) {
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "cancelled") {
		return "", nil
	}
	return path, err
}

func nativeKind(kind string) bool {
	for _, item := range nativeKinds {
		if item == kind {
			return true
		}
	}
	return false
}

func ptr(v int) *int { return &v }

func unknownIfEmpty(v string) string {
	if v == "" {
		return "unknown"
	}
	return v
}

type exportDoc struct {
	Kind                   string `json:"kind"`
	AppVersion             string `json:"appVersion"`
	WebView2Runtime        string `json:"webview2Runtime"`
	IMEMode                string `json:"imeMode"`
	NativeFocusMeaning     string `json:"nativeFocusMeaning"`
	FrozenBeforeExportUI   bool   `json:"frozenBeforeExportUI"`
	MenuMayPrecedeSnapshot bool   `json:"menuMayPrecedeSnapshot"`
	Note                   string `json:"note"`
	DOM                    struct {
		Available bool            `json:"available"`
		Truncated bool            `json:"truncated,omitempty"`
		Events    json.RawMessage `json:"events,omitempty"`
	} `json:"dom"`
	Native struct {
		Events []NativeEvent `json:"events"`
	} `json:"native"`
	AfterExportUI struct {
		Native []NativeEvent `json:"native"`
	} `json:"afterExportUI"`
}

type domEvent struct {
	T                int64    `json:"t"`
	Seq              int64    `json:"seq"`
	Kind             string   `json:"kind"`
	Tag              string   `json:"tag,omitempty"`
	Connected        *bool    `json:"connected,omitempty"`
	Editable         *bool    `json:"editable,omitempty"`
	Rect             *domRect `json:"rect,omitempty"`
	VW               *int     `json:"vw,omitempty"`
	VH               *int     `json:"vh,omitempty"`
	DPR              *float64 `json:"dpr,omitempty"`
	Phase            string   `json:"phase,omitempty"`
	Side             string   `json:"side,omitempty"`
	DefaultPrevented *bool    `json:"defaultPrevented,omitempty"`
	DocumentHasFocus *bool    `json:"documentHasFocus,omitempty"`
}

type domRect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type domSnap struct {
	FrozenBeforeExportUI bool       `json:"frozenBeforeExportUI"`
	Truncated            bool       `json:"truncated,omitempty"`
	Events               []domEvent `json:"events"`
}

func cleanSnapshot(payload string) (json.RawMessage, error) {
	snap, err := decodeSnap(payload)
	if err != nil {
		return nil, err
	}
	if !snap.FrozenBeforeExportUI || len(snap.Events) > RingCap {
		return nil, errSchema
	}
	for _, ev := range snap.Events {
		if err := validateEvent(ev); err != nil {
			return nil, err
		}
	}
	return json.Marshal(snap)
}

func cleanDOM(raw json.RawMessage) (json.RawMessage, bool, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, false, errSchema
	}
	if raw[0] == '[' {
		var events []domEvent
		if err := decodeInto(raw, &events); err != nil {
			return nil, false, err
		}
		out, err := marshalEvents(events)
		return out, false, err
	}
	snap, err := decodeSnap(string(raw))
	if err != nil {
		return nil, false, err
	}
	out, err := marshalEvents(snap.Events)
	return out, snap.Truncated, err
}

func marshalEvents(events []domEvent) (json.RawMessage, error) {
	if len(events) > RingCap {
		return nil, errSchema
	}
	for _, ev := range events {
		if err := validateEvent(ev); err != nil {
			return nil, err
		}
	}
	if events == nil {
		events = []domEvent{}
	}
	return json.Marshal(events)
}

func decodeSnap(payload string) (domSnap, error) {
	var snap domSnap
	if err := decodeInto([]byte(payload), &snap); err != nil {
		return domSnap{}, err
	}
	return snap, nil
}

func decodeInto(raw []byte, dest any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return err
	}
	if dec.More() {
		return errSchema
	}
	return nil
}

func validateEvent(ev domEvent) error {
	if ev.T < 0 || ev.Seq < 1 || !domKind(ev.Kind) || !domTag(ev.Tag) {
		return errSchema
	}
	if ev.Kind == "shift" {
		if (ev.Phase != "down" && ev.Phase != "up") || !shiftSide(ev.Side) || ev.DefaultPrevented == nil || ev.DocumentHasFocus != nil {
			return errSchema
		}
		return nil
	}
	if ev.Kind == "window-focus" || ev.Kind == "window-blur" {
		if ev.DocumentHasFocus == nil || ev.Phase != "" || ev.Side != "" || ev.DefaultPrevented != nil {
			return errSchema
		}
		return nil
	}
	if ev.Phase != "" || ev.Side != "" || ev.DefaultPrevented != nil || ev.DocumentHasFocus != nil {
		return errSchema
	}
	return nil
}

func domKind(kind string) bool {
	switch kind {
	case "focus", "blur", "compositionstart", "compositionend", "compositionupdate", "shift", "resize", "window-focus", "window-blur":
		return true
	default:
		return false
	}
}

func domTag(tag string) bool {
	switch tag {
	case "", "TEXTAREA", "INPUT", "DIV", "BODY", "other":
		return true
	default:
		return false
	}
}

func shiftSide(side string) bool {
	return side == "left" || side == "right" || side == "unspecified"
}
