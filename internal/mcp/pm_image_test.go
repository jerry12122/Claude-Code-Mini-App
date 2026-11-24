package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/media"
)

func TestPMRejectTruncatedPNG(t *testing.T) {
	d, s, w := receiptDB(t)
	p := filepath.Join(w, "broken.png")
	os.WriteFile(p, receiptPNG[:8], 0600)
	_, out, err := d.postMessage(context.Background(), nil, postMessageIn{SessionID: s.ID, ImagePaths: []string{p}})
	if err == nil && out.Delivery == "stored_verified" {
		t.Fatal("8-byte PNG signature is not a decodable picture, but stored_verified returned")
	}
}

func TestPMByteMismatchNotVerified(t *testing.T) {
	d, s, w := receiptDB(t)
	p := filepath.Join(w, "shot.png")
	os.WriteFile(p, receiptPNG, 0600)
	out := postImage(t, d, s.ID, "shot", []string{p}, nil)
	msg, err := d.db.MessageByID(s.ID, out.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	a := msg.Attachments[0]
	saved, err := media.ExistingUploadPath(a.SessionID, a.StorageName)
	if err != nil {
		t.Fatal(err)
	}
	data := append([]byte(nil), receiptPNG...)
	data[40] ^= 1
	os.WriteFile(saved, data, 0600)
	expected := map[string]string{a.ID: digestHex(receiptPNG)}
	if _, _, err := confirmStoredImages(d.db, s.ID, msg, expected); err == nil {
		t.Fatal("same size different bytes accepted as verified")
	}
}

func TestPMRollbackKeepsReferencedFreshImage(t *testing.T) {
	d, s, w := receiptDB(t)
	p := filepath.Join(w, "shot.png")
	os.WriteFile(p, receiptPNG, 0600)
	first := postImage(t, d, s.ID, "one", []string{p}, nil)
	id := first.Attachments[0].ID
	second, err := d.db.BindImageMessage(s.ID, "claude", "two", "", nil, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.db.DeleteImageMessage(s.ID, first.MessageID, []string{id}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.db.MessageByID(s.ID, second.ID); err != nil {
		t.Fatal("rollback deleted attachment referenced by another message:", err)
	}
	_ = db.Attachment{}
}
