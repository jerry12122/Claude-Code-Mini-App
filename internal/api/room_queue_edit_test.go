package api

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

func TestRoomQueueEditValidationAndDependency(t *testing.T) {
	e := newRoomsEnv(t)
	h := NewRoomsHandler(e.db, e.quota)
	e.app.Patch("/rooms/:id/messages/:queue_id", h.EditQueuedMessage)
	pm := e.session("PM", "claude")
	rid := e.create("編輯", member{"session_id": pm.ID, "role": "pm"})
	path := "/rooms/" + rid + "/messages/1"
	valid := map[string]any{"expected_pm_id": pm.ID, "previous": "", "content": "新文字"}
	status, _ := e.do("PATCH", path, valid)
	if status != 503 {
		t.Fatalf("未注入鎖路徑不可直接寫 DB：%d", status)
	}
	calls := 0
	h.EditQueued = func(_ context.Context, roomID, expectedPM string, id int64, previous, content string) error {
		calls++
		if roomID != rid || expectedPM != pm.ID || id != 1 || previous != "" || content != "新文字" {
			t.Fatal("字串／scope 被改寫")
		}
		return nil
	}
	for _, field := range []string{"expected_pm_id", "previous", "content"} {
		for _, bad := range []any{nil, 42, true, []string{"x"}, map[string]any{}} {
			t.Run(field, func(t *testing.T) {
				body := map[string]any{"expected_pm_id": pm.ID, "previous": "", "content": "新文字"}
				body[field] = bad
				if code, _ := e.do("PATCH", path, body); code != 400 {
					t.Fatalf("null／非字串應400：%d", code)
				}
			})
		}
		body := map[string]any{"expected_pm_id": pm.ID, "previous": "", "content": "新文字"}
		delete(body, field)
		if code, _ := e.do("PATCH", path, body); code != 400 {
			t.Fatalf("缺少 %s：%d", field, code)
		}
	}
	for _, raw := range []string{"null", "[]", "{", `{"expected_pm_id":"x","previous":"","content":"\u0000"}`, `{"expected_pm_id":"x","previous":"","content":"` + string([]byte{0xff}) + `"}`} {
		if code, _ := e.do("PATCH", path, raw); code != 400 {
			t.Fatalf("無效JSON／UTF8／NUL：%d", code)
		}
	}
	for _, id := range []string{"0", "-1", "abc", "1.5", "1e2", "9223372036854775808"} {
		if code, _ := e.do("PATCH", "/rooms/"+rid+"/messages/"+id, valid); code != 400 {
			t.Fatalf("非法queue id：%d", code)
		}
	}
	if code, _ := e.do("PATCH", path, map[string]string{"expected_pm_id": " ", "previous": "", "content": "x"}); code != 400 {
		t.Fatalf("空PM：%d", code)
	}
	if code, _ := e.do("PATCH", path, map[string]string{"expected_pm_id": pm.ID, "previous": "", "content": strings.Repeat("x", 64001)}); code != 400 {
		t.Fatalf("既有64000 bytes上限：%d", code)
	}
	if calls != 0 {
		t.Fatal("無效請求不得進入寫入helper")
	}
	if code, _ := e.do("PATCH", path, valid); code != 204 || calls != 1 {
		t.Fatalf("明確空previous為合法字串：%d calls=%d", code, calls)
	}
	for _, tc := range []struct {
		err  error
		want int
	}{
		{db.ErrQueueEditInvalid, 400}, {db.ErrInvalidAttachment, 400}, {db.ErrQueueEditForbidden, 404},
		{db.ErrQueueEditConflict, 409}, {db.ErrRoomPMChanged, 409}, {db.ErrRoomPM, 409}, {errors.New("private database detail"), 500},
	} {
		h.EditQueued = func(context.Context, string, string, int64, string, string) error { return tc.err }
		code, body := e.do("PATCH", path, valid)
		if code != tc.want || strings.Contains(string(body), "private database detail") {
			t.Fatalf("錯誤映射：%d want%d", code, tc.want)
		}
	}
	if code, _ := e.do("PATCH", "/rooms/missing/messages/1", valid); code != 404 {
		t.Fatalf("無room應404：%d", code)
	}
}
