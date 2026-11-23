package imediag

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNativeRingOrderCapAndFreeze(t *testing.T) {
	log := New()
	for i := 0; i < 300; i++ {
		log.AddNative(nativeKinds[i%len(nativeKinds)], Geo{})
	}
	log.AddNative("dpi", Geo{Known: true, X: 4, Y: 5, W: 6, H: 7})
	frozen := log.FreezeNative()
	if len(frozen) != RingCap {
		t.Fatalf("cap %d", len(frozen))
	}
	if frozen[0].Kind != nativeKinds[45%len(nativeKinds)] || frozen[len(frozen)-1].Kind != "dpi" {
		t.Fatalf("order %s .. %s", frozen[0].Kind, frozen[len(frozen)-1].Kind)
	}
	if frozen[len(frozen)-1].DPI != "unknown" || frozen[len(frozen)-1].X == nil || *frozen[len(frozen)-1].X != 4 {
		t.Fatalf("dpi/geo %+v", frozen[len(frozen)-1])
	}
	beforeLen, lastKind := len(frozen), frozen[len(frozen)-1].Kind
	log.AddNative("show", Geo{})
	if len(frozen) != beforeLen || frozen[len(frozen)-1].Kind != lastKind {
		t.Fatal("frozen snapshot changed")
	}
	after := log.NativeAfter()
	if len(after) != 1 || after[0].Kind != "show" {
		t.Fatalf("after %+v", after)
	}
	log.AddNative("key-SECRET-KEY", Geo{})
	if containsKind(log.NativeAfter(), "key-SECRET-KEY") {
		t.Fatal("non native kind stored")
	}
}

func TestAcceptRejectsSensitiveAndUntrusted(t *testing.T) {
	log := New()
	home := "https://owner.test"
	if err := log.Accept(in(home, validBody(focusEvent))); !errors.Is(err, errNotArmed) {
		t.Fatalf("unarmed %v", err)
	}
	log.Arm()
	planted := validBody(`{"t":1,"seq":1,"kind":"focus","data":"SECRET-DATA","value":"SECRET-VALUE","key":"SECRET-KEY","url":"SECRET-URL","id":"SECRET-DOM-ID"}`)
	if err := log.Accept(in(home, planted)); !errors.Is(err, errSchema) || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("schema %v", err)
	}
	if raw, ok := log.DOM(); ok || strings.Contains(string(raw), "SECRET") {
		t.Fatal("secret retained")
	}
	if err := log.Accept(in(home, validBody(focusEvent))); err != nil {
		t.Fatal(err)
	}
	raw, ok := log.DOM()
	if !ok || strings.Contains(string(raw), "SECRET") || !strings.Contains(string(raw), `"kind":"focus"`) {
		t.Fatalf("dom %s", raw)
	}
	log.Arm()
	if err := log.Accept(in(home, validBody(focusEvent))); !errors.Is(err, errRate) {
		t.Fatalf("rate %v", err)
	}
	nowFn = func() time.Time { return time.Unix(10, 0) }
	t.Cleanup(func() { nowFn = time.Now })
	log.Arm()
	if err := log.Accept(in(home, validBody(shiftEvent))); err != nil {
		t.Fatal(err)
	}
	shift, _ := log.DOM()
	if strings.Contains(string(shift), "Shift") || !strings.Contains(string(shift), `"phase":"down"`) || !strings.Contains(string(shift), `"side":"left"`) {
		t.Fatalf("shift %s", shift)
	}
	log.Arm()
	nowFn = func() time.Time { return time.Unix(20, 0) }
	evil := in(home, validBody(focusEvent))
	evil.Origin = "https://evil.test"
	if err := log.Accept(evil); !errors.Is(err, errOrigin) {
		t.Fatalf("origin %v", err)
	}
	if err := log.Accept(Inbound{WindowName: "other", Origin: home, TopOrigin: home, Home: home, Body: validBody(focusEvent)}); !errors.Is(err, errWindow) {
		t.Fatalf("window %v", err)
	}
	if err := log.Accept(in(home, "ime-diag:"+strings.Repeat("x", MaxMessageBytes))); !errors.Is(err, errSize) {
		t.Fatalf("size %v", err)
	}
}

func TestBuildAndCancel(t *testing.T) {
	log := New()
	log.AddNative("active", Geo{})
	native := log.FreezeNative()
	body, err := Build(nil, false, native, []NativeEvent{{Kind: "set-focus"}}, Meta{})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["imeMode"] != "unknown" || doc["webview2Runtime"] != "unknown" || doc["frozenBeforeExportUI"] != true {
		t.Fatalf("doc %s", body)
	}
	if strings.Contains(string(body), "SECRET") {
		t.Fatal("secret in empty export")
	}
	wrote := false
	ok, err := Commit(body, func() (string, error) { return "", nil }, func(string, []byte) error {
		wrote = true
		return nil
	})
	if err != nil || ok || wrote {
		t.Fatalf("cancel ok=%v wrote=%v err=%v", ok, wrote, err)
	}
	path, err := NormalizeChoice("", errors.New("cancelled by user"))
	if err != nil || path != "" {
		t.Fatalf("normalize %q %v", path, err)
	}
	ok, err = Commit(body, func() (string, error) { return "chosen.json", nil }, func(p string, data []byte) error {
		wrote = p == "chosen.json" && string(data) == string(body)
		return nil
	})
	if err != nil || !ok || !wrote {
		t.Fatalf("write ok=%v wrote=%v err=%v", ok, wrote, err)
	}
	if _, err := Build(json.RawMessage(`[{"data":"SECRET-DATA"}]`), true, nil, nil, Meta{AppVersion: "0.24.0"}); err == nil {
		t.Fatal("raw secret accepted")
	}
	if Script("") != "" || !strings.Contains(Script("https://owner.test"), "imeDiagAuto(window)") {
		t.Fatal("script guard")
	}
	if strings.Contains(Script(`</script><script>alert(1)`), `</script>`) {
		t.Fatal("owner breakout")
	}
}

func TestAfterStaysBoundedAndStops(t *testing.T) {
	log := New()
	frozen := log.FreezeNative()
	for i := 0; i < RingCap*4; i++ {
		log.AddNative("move", Geo{})
	}
	if got := len(log.NativeAfter()); got > RingCap {
		t.Fatalf("afterExportUI buffer unbounded: got=%d limit=%d", got, RingCap)
	}
	if len(frozen) != 0 {
		t.Fatal("frozen copy grew")
	}
	log.EndExport()
	for i := 0; i < RingCap*4; i++ {
		log.AddNative("move", Geo{})
	}
	if got := len(log.NativeAfter()); got != 0 {
		t.Fatalf("after collection survived export: %d", got)
	}
	if got := len(log.FreezeNative()); got != RingCap {
		t.Fatalf("main ring %d", got)
	}
}

func TestSecondExportDoesNotClearSnapshot(t *testing.T) {
	log := New()
	if !log.BeginExport() {
		t.Fatal("begin")
	}
	log.FreezeNative()
	log.Arm()
	if err := log.Accept(in("https://owner.test", validBody(focusEvent))); err != nil {
		t.Fatal(err)
	}
	if log.BeginExport() {
		t.Fatal("second export")
	}
	raw, ok := log.DOM()
	if !ok || !strings.Contains(string(raw), `"kind":"focus"`) {
		t.Fatal("snapshot cleared")
	}
	log.AddNative("move", Geo{})
	log.EndExport()
	log.AddNative("resize", Geo{})
	if len(log.NativeAfter()) != 0 {
		t.Fatal("after collection survived export")
	}
	if !log.BeginExport() {
		t.Fatal("locked after end")
	}
	log.EndExport()
}

func TestWindowFocusSchema(t *testing.T) {
	log := New()
	home := "https://owner.test"
	log.Arm()
	if err := log.Accept(in(home, validBody(windowBlurEvent))); err != nil {
		t.Fatal(err)
	}
	raw, ok := log.DOM()
	if !ok || !strings.Contains(string(raw), `"kind":"window-blur"`) || !strings.Contains(string(raw), `"documentHasFocus":false`) {
		t.Fatalf("window %s", raw)
	}
	if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "HWND") {
		t.Fatal("window record leaked or claimed native focus")
	}
	nowFn = func() time.Time { return time.Unix(30, 0) }
	t.Cleanup(func() { nowFn = time.Now })
	log.Arm()
	planted := validBody(`{"t":5,"seq":5,"kind":"focus","documentHasFocus":true}`)
	if err := log.Accept(in(home, planted)); !errors.Is(err, errSchema) {
		t.Fatalf("element focus borrowed documentHasFocus: %v", err)
	}
	log.Arm()
	if err := log.Accept(in(home, validBody(`{"t":6,"seq":6,"kind":"window-focus","tag":"TEXTAREA"}`))); !errors.Is(err, errSchema) {
		t.Fatalf("window focus without documentHasFocus: %v", err)
	}
}

func in(home, body string) Inbound {
	return Inbound{WindowName: "main", Origin: home, TopOrigin: home, Home: home, Body: body}
}

func validBody(events string) string {
	return `ime-diag:{"frozenBeforeExportUI":true,"events":[` + events + `]}`
}

const focusEvent = `{"t":1,"seq":1,"kind":"focus","tag":"TEXTAREA","connected":true,"editable":true,"rect":{"x":1,"y":2,"w":3,"h":4},"vw":800,"vh":600,"dpr":1.5}`
const windowBlurEvent = `{"t":4,"seq":4,"kind":"window-blur","tag":"TEXTAREA","connected":true,"editable":true,"documentHasFocus":false,"rect":{"x":1,"y":2,"w":3,"h":4}}`
const shiftEvent = `{"t":2,"seq":2,"kind":"shift","phase":"down","side":"left","defaultPrevented":false,"tag":"TEXTAREA","connected":true,"editable":true}`

func containsKind(events []NativeEvent, kind string) bool {
	for _, e := range events {
		if e.Kind == kind {
			return true
		}
	}
	return false
}
