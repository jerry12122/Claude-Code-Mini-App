@echo off
go generate ./cmd/desktop
if errorlevel 1 exit /b %errorlevel%
go build -tags "production,devtools" -ldflags "-H windowsgui" -o claude-miniapp-desktop.exe ./cmd/desktop
