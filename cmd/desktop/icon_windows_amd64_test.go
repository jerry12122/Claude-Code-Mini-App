package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
	"testing"
	"unsafe"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/version"
	"golang.org/x/sys/windows"
)

func TestDesktopExecutableIcon(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	file, err := pe.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	section := file.Section(".rsrc")
	if section == nil {
		t.Fatal("EXE 缺少 Windows 圖示資源，釘選後會遺失狗狗圖示")
	}
	resources, err := section.Data()
	if err != nil {
		t.Fatal(err)
	}
	icon, err := os.ReadFile("../../internal/static/favicon.ico")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < int(binary.LittleEndian.Uint16(icon[4:6])); i++ {
		entry := icon[6+i*16 : 6+(i+1)*16]
		size := binary.LittleEndian.Uint32(entry[8:12])
		offset := binary.LittleEndian.Uint32(entry[12:16])
		if !bytes.Contains(resources, icon[offset:offset+size]) {
			t.Fatal("EXE 圖示與 favicon.ico 不一致，請執行 go generate ./cmd/desktop")
		}
	}
	name, err := syscall.UTF16PtrFromString(exe)
	if err != nil {
		t.Fatal(err)
	}
	count, _, callErr := syscall.NewLazyDLL("shell32.dll").NewProc("ExtractIconExW").Call(uintptr(unsafe.Pointer(name)), 0xffffffff, 0, 0, 0)
	if count != 1 {
		t.Fatalf("Windows 無法從 EXE 讀到唯一的應用程式圖示: count=%d error=%v", count, callErr)
	}
}

func TestDesktopExecutableVersion(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	size, err := windows.GetFileVersionInfoSize(exe, nil)
	if err != nil {
		t.Fatalf("missing Windows version resource: %v", err)
	}
	data := make([]byte, size)
	if err := windows.GetFileVersionInfo(exe, 0, size, unsafe.Pointer(&data[0])); err != nil {
		t.Fatal(err)
	}
	var info *windows.VS_FIXEDFILEINFO
	var length uint32
	if err := windows.VerQueryValue(unsafe.Pointer(&data[0]), `\`, unsafe.Pointer(&info), &length); err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprintf("%d.%d.%d.%d", info.FileVersionMS>>16, info.FileVersionMS&0xffff, info.FileVersionLS>>16, info.FileVersionLS&0xffff)
	if got != version.Version+".0" {
		t.Fatalf("Windows version = %s, runtime version = %s; run go generate ./cmd/desktop", got, version.Version)
	}
}
