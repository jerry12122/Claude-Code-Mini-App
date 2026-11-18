//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	desktopMutex     = `Local\ClaudeMiniApp.Desktop`
	maintenanceMutex = `Local\ClaudeMiniApp.Setup`
	uninstallKey     = `Software\Microsoft\Windows\CurrentVersion\Uninstall\ClaudeMiniApp_is1`
)

// An explicit marker keeps installed storage independent of shortcut working directories.
func setDesktopWorkDir(exeDir, cwd, localAppData string) (string, error) {
	markerFile, err := os.Open(filepath.Join(exeDir, "installed.flag"))
	if errors.Is(err, os.ErrNotExist) {
		if dir := firstRunWorkDir(exeDir, cwd, fileExists); dir != "" {
			return "", os.Chdir(dir)
		}
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("讀取安裝標記失敗: %w", err)
	}
	defer markerFile.Close()
	marker, err := markerFile.Stat()
	if err != nil {
		return "", err
	}
	if !marker.Mode().IsRegular() {
		return "", errors.New("安裝標記 installed.flag 不是一般檔案")
	}
	if !filepath.IsAbs(localAppData) {
		return "", errors.New("無法取得 LOCALAPPDATA 的絕對路徑")
	}
	dataDir := filepath.Join(localAppData, "ClaudeMiniApp", "data")
	profile := filepath.Join(dataDir, "webview2")
	if err := os.MkdirAll(profile, 0700); err != nil {
		return "", fmt.Errorf("建立使用者資料目錄失敗: %w", err)
	}
	if err := os.Chdir(dataDir); err != nil {
		return "", fmt.Errorf("切換使用者資料目錄失敗: %w", err)
	}
	return profile, nil
}

// Keep a handle, not ownership: setup only needs to detect the desktop's lifetime.
func holdDesktopMutex(appName, maintenanceName string) (func(), error) {
	app, err := windows.UTF16PtrFromString(appName)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateMutex(nil, false, app)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return nil, err
	}
	release := func() { _ = windows.CloseHandle(h) }
	maintenance, err := windows.UTF16PtrFromString(maintenanceName)
	if err != nil {
		release()
		return nil, err
	}
	other, err := windows.OpenMutex(windows.SYNCHRONIZE, false, maintenance)
	if err == nil {
		_ = windows.CloseHandle(other)
		release()
		return nil, errors.New("安裝或解除安裝正在進行，請完成後再開啟 MiniApp")
	}
	if !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		release()
		return nil, err
	}
	return release, nil
}

// Open only: running a copied executable must not recreate an uninstalled product.
func syncInstalledVersion(keyPath, currentVersion string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.SET_VALUE|registry.WOW64_64KEY)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return nil
	}
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue("DisplayVersion", currentVersion)
}
