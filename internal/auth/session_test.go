package auth

import (
	"testing"
	"time"
)

func TestValidate_ValidToken(t *testing.T) {
	store := NewStore(time.Hour)
	token, err := store.Create(0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	valid, tgID := store.Validate(token)
	if !valid {
		t.Fatal("expected valid token")
	}
	if tgID != 0 {
		t.Fatalf("tgID = %d, want 0", tgID)
	}
}

func TestValidate_ExpiredToken(t *testing.T) {
	store := NewStore(time.Millisecond)
	token, err := store.Create(0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	valid, _ := store.Validate(token)
	if valid {
		t.Fatal("expected expired token to be invalid")
	}
	// 過期後 entry 應已刪除，再次驗證仍無效
	valid, _ = store.Validate(token)
	if valid {
		t.Fatal("expected deleted expired token to remain invalid")
	}
}

func TestValidate_UnknownToken(t *testing.T) {
	store := NewStore(time.Hour)
	valid, _ := store.Validate("unknown-token-abc123")
	if valid {
		t.Fatal("expected unknown token to be invalid")
	}
}

func TestValidate_EmptyToken(t *testing.T) {
	store := NewStore(time.Hour)
	valid, _ := store.Validate("")
	if valid {
		t.Fatal("expected empty token to be invalid")
	}
}

func TestDelete(t *testing.T) {
	store := NewStore(time.Hour)
	token, err := store.Create(42)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	store.Delete(token)
	valid, tgID := store.Validate(token)
	if valid {
		t.Fatal("expected deleted token to be invalid")
	}
	if tgID != 0 {
		t.Fatalf("tgID = %d, want 0", tgID)
	}
}
