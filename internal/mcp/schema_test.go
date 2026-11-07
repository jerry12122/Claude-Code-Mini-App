package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Legacy MCP clients reject boolean property schemas and discard the entire tools/list.
func TestToolsListPendingSchemasUseObjects(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/schema.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	server := httptest.NewServer(NewHTTPHandler(database, nil, NewRegistry(nil, nil), 0))
	defer server.Close()
	ctx := context.Background()
	client, err := gomcp.NewClient(&gomcp.Implementation{Name: "schema-test", Version: "1"}, nil).Connect(ctx, &gomcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	list, err := client.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, tool := range list.Tools {
		if tool.Name != "get_status" && tool.Name != "ask_session" {
			continue
		}
		data, err := json.Marshal(tool.OutputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(data, &schema); err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"pending_question", "pending_permission"} {
			var object map[string]any
			if err := json.Unmarshal(schema.Properties[field], &object); err != nil || object == nil {
				t.Errorf("%s.%s must serialize as a schema object, got %s", tool.Name, field, schema.Properties[field])
			}
			checked++
		}
	}
	if checked != 4 {
		t.Fatalf("checked %d schemas, want 4", checked)
	}
}

// 回歸：pending_permission 以前是 json.RawMessage，schema 被推導成整數陣列，
// 實際內容（物件陣列）讓 SDK 的輸出驗證失敗，get_status 在等授權時直接報錯。
func TestStatusOutputSchemaAcceptsPermissionObjects(t *testing.T) {
	perm := rawToAny(json.RawMessage(`[{"tool_name":"WebSearch","tool_input":{"query":"x"}}]`))
	check := func(name string, schema *jsonschema.Schema, err error, out any) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		rs, err := schema.Resolve(nil)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(out)
		var v map[string]any
		_ = json.Unmarshal(b, &v)
		if err := rs.Validate(v); err != nil {
			t.Fatalf("%s 輸出驗證失敗: %v", name, err)
		}
	}
	s1, err := jsonschema.For[getStatusOut](nil)
	check("get_status", s1, err, getStatusOut{State: StateAwaitingPermission, PendingPermission: perm, Connected: true})
	s2, err := jsonschema.For[askSessionOut](nil)
	check("ask_session", s2, err, askSessionOut{State: StateAwaitingPermission, PendingPermission: perm})

	if rawToAny(nil) != nil {
		t.Fatal("空的 pending_permission 應為 nil（omitempty 才會省略）")
	}
}
