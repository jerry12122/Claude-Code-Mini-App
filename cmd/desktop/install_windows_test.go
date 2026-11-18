//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func TestInstalledWorkDir(t *testing.T) {
	root := t.TempDir()
	exeDir := filepath.Join(root, "program")
	launchDir := filepath.Join(root, "launch")
	local := filepath.Join(root, "使用者 空白")
	for _, dir := range []string{exeDir, launchDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(launchDir)
	if err := os.WriteFile(filepath.Join(exeDir, "installed.flag"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	// Installed mode wins even if a stray portable config exists beside the EXE.
	if err := os.WriteFile(filepath.Join(exeDir, "config.yaml"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	profile, err := setDesktopWorkDir(exeDir, launchDir, local)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(local, "ClaudeMiniApp", "data")
	if got, _ := os.Getwd(); got != want {
		t.Fatalf("cwd = %q, want %q", got, want)
	}
	if profile != filepath.Join(want, "webview2") {
		t.Fatalf("profile = %q", profile)
	}
	if st, err := os.Stat(profile); err != nil || !st.IsDir() {
		t.Fatalf("profile not created: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(exeDir, "config.yaml")); string(b) != "keep" {
		t.Fatal("portable config changed")
	}
}

func TestInstalledWorkDirFailsClosed(t *testing.T) {
	for _, problem := range []string{"missing-local", "relative-local", "blocked-data", "marker-directory", "blocked-marker"} {
		t.Run(problem, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			exeDir := filepath.Join(root, "program")
			if err := os.Mkdir(exeDir, 0700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(exeDir, "installed.flag")
			if err := os.WriteFile(marker, nil, 0600); err != nil {
				t.Fatal(err)
			}
			local := filepath.Join(root, "local")
			switch problem {
			case "missing-local":
				local = ""
			case "relative-local":
				local = "relative"
			case "blocked-data":
				if err := os.WriteFile(local, nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "marker-directory":
				if err := os.Remove(marker); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(marker, 0700); err != nil {
					t.Fatal(err)
				}
			case "blocked-marker":
				p, _ := windows.UTF16PtrFromString(marker)
				h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, 0, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer windows.CloseHandle(h)
			}
			if _, err := setDesktopWorkDir(exeDir, root, local); err == nil {
				t.Fatal("expected startup error")
			}
			if got, _ := os.Getwd(); got != root {
				t.Fatalf("failure changed cwd: %q", got)
			}
		})
	}
}

func TestUnmarkedWorkDir(t *testing.T) {
	root := t.TempDir()
	exeDir := filepath.Join(root, "program")
	if err := os.Mkdir(exeDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	if p, err := setDesktopWorkDir(exeDir, root, ""); err != nil || p != "" {
		t.Fatalf("development: %q %v", p, err)
	}
	if got, _ := os.Getwd(); got != root {
		t.Fatalf("development cwd: %q", got)
	}
	if err := os.WriteFile(filepath.Join(exeDir, "config.example.yaml"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if p, err := setDesktopWorkDir(exeDir, root, ""); err != nil || p != "" {
		t.Fatalf("portable: %q %v", p, err)
	}
	if got, _ := os.Getwd(); got != exeDir {
		t.Fatalf("portable cwd: %q", got)
	}
}

func TestDesktopMaintenanceMutex(t *testing.T) {
	name := `Local\ClaudeMiniApp.Test.` + filepath.Base(filepath.Dir(t.TempDir()))
	app, maintenance := name+".App", name+".Setup"
	release, err := holdDesktopMutex(app, maintenance)
	if err != nil {
		t.Fatal(err)
	}
	second, err := holdDesktopMutex(app, maintenance)
	if err != nil {
		t.Fatal("existing desktop handle should preserve single-instance behavior:", err)
	}
	second()
	ptr, _ := windows.UTF16PtrFromString(app)
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, ptr)
	if err != nil {
		t.Fatal("desktop mutex missing:", err)
	}
	windows.CloseHandle(h)
	release()
	ptr, _ = windows.UTF16PtrFromString(maintenance)
	h, err = windows.CreateMutex(nil, false, ptr)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	if release, err = holdDesktopMutex(app, maintenance); err == nil {
		release()
		t.Fatal("maintenance should reject startup")
	}
	if _, err := holdDesktopMutex(app, "bad\x00mutex"); err == nil {
		t.Fatal("mutex errors must fail closed")
	}
	if _, err := holdDesktopMutex("bad\x00mutex", maintenance); err == nil {
		t.Fatal("invalid desktop mutex must fail closed")
	}
	ptr, _ = windows.UTF16PtrFromString(app)
	if h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, ptr); err == nil {
		windows.CloseHandle(h)
		t.Fatal("failed startup leaked desktop mutex")
	}
}

func TestInstalledVersionRegistry(t *testing.T) {
	path := `Software\ClaudeMiniAppTests\` + filepath.Base(filepath.Dir(t.TempDir()))
	if err := syncInstalledVersion(path, "1.0.1"); err != nil {
		t.Fatal(err)
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.READ); err == nil {
		k.Close()
		t.Fatal("missing uninstall entry was recreated")
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.ALL_ACCESS|registry.WOW64_64KEY)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.DeleteKey(registry.CURRENT_USER, path)
	defer k.Close()
	if err := k.SetStringValue("DisplayName", "Keep"); err != nil {
		t.Fatal(err)
	}
	if err := syncInstalledVersion(path, "1.0.1"); err != nil {
		t.Fatal(err)
	}
	if value, _, err := k.GetStringValue("DisplayVersion"); err != nil || value != "1.0.1" {
		t.Fatalf("version = %q %v", value, err)
	}
	if value, _, _ := k.GetStringValue("DisplayName"); value != "Keep" {
		t.Fatal("other uninstall metadata changed")
	}
	if err := syncInstalledVersion("bad\x00key", "1.0.1"); err == nil {
		t.Fatal("registry error was swallowed")
	}
}
