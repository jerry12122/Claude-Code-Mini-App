package mcp

import (
	"context"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/media"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

func TestPMConcurrentRequestCannotOverwriteExpectedImage(t *testing.T) {
	d, s, w := receiptDB(t)
	red := solidPNG(t, color.RGBA{R: 255, A: 255})
	blue := solidPNG(t, color.RGBA{B: 255, A: 255})
	src := filepath.Join(w, "red.png")
	os.WriteFile(src, red, 0600)
	previous := afterImageCommit
	t.Cleanup(func() { afterImageCommit = previous })
	afterImageCommit = func() {
		msgs, err := d.db.ListMessages(s.ID)
		if err != nil {
			t.Fatal(err)
		}
		msg, err := d.db.MessageByID(s.ID, msgs[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		a := msg.Attachments[0]
		file, err := media.ExistingUploadPath(a.SessionID, a.StorageName)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(file, blue, 0600)
		if err := verifyExistingImages(d.db, s.ID, []string{a.ID}); err != nil {
			t.Fatal(err)
		}
	}
	_, out, err := d.postMessage(context.Background(), nil, postMessageIn{SessionID: s.ID, ImagePaths: []string{src}})
	if err == nil && out.Delivery == "stored_verified" {
		t.Fatal("another request replaced this delivery's expected hash; red input falsely verified as blue")
	}
}

func TestPMCleanupDBErrorMustKeepFile(t *testing.T) {
	d, s, w := receiptDB(t)
	src := filepath.Join(w, "keep.png")
	os.WriteFile(src, receiptPNG, 0600)
	fresh, files, err := stageChatImages(s, []string{src})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.db.BindImageMessage(s.ID, "claude", "kept", "", fresh, []string{fresh[0].ID}); err != nil {
		t.Fatal(err)
	}
	d.db.Close()
	removeUnreferenced(d.db, s.ID, fresh, files)
	if _, err = os.Stat(files[0]); err != nil {
		t.Fatal("database lookup failure was treated as proof of no references; file deleted")
	}
}
