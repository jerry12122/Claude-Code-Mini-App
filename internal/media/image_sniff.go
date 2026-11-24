package media

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

const (
	maxChatEdge   = 8192
	maxChatPixels = 4096 * 4096
)

// ChatImageLimit 是聊天圖片單張上限，與截圖落地共用。
func ChatImageLimit() int { return maxImageBytes }

// SniffChatImage 接受可完整解碼的 png、jpeg、gif。只對檔頭不算；webp 沒有解碼器，直接拒絕。
func SniffChatImage(data []byte) (mime, ext string, err error) {
	if len(data) == 0 {
		return "", "", fmt.Errorf("圖片是空的")
	}
	if len(data) > maxImageBytes {
		return "", "", fmt.Errorf("圖片超過大小上限")
	}
	mime, ext, format, err := chatImageKind(data)
	if err != nil {
		return "", "", err
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || imageTooLarge(cfg.Width, cfg.Height) {
		return "", "", fmt.Errorf("圖片無法解碼或尺寸超過上限")
	}
	img, got, err := image.Decode(bytes.NewReader(data))
	if err != nil || img == nil || got != format || img.Bounds().Dx() != cfg.Width || img.Bounds().Dy() != cfg.Height {
		return "", "", fmt.Errorf("圖片無法解碼")
	}
	return mime, ext, nil
}

func chatImageKind(data []byte) (mime, ext, format string, err error) {
	switch {
	case len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return "image/png", ".png", "png", nil
	case len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff:
		return "image/jpeg", ".jpg", "jpeg", nil
	case len(data) >= 6 && (bytes.Equal(data[:6], []byte("GIF87a")) || bytes.Equal(data[:6], []byte("GIF89a"))):
		return "image/gif", ".gif", "gif", nil
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "", "", "", fmt.Errorf("webp 沒有解碼器，不接受")
	default:
		return "", "", "", fmt.Errorf("不是可傳送的圖片")
	}
}

func imageTooLarge(w, h int) bool {
	if w <= 0 || h <= 0 || w > maxChatEdge || h > maxChatEdge {
		return true
	}
	return int64(w)*int64(h) > maxChatPixels
}
