//go:build !windows

package fsbrowse

import "path/filepath"

func platformNameOK(string) bool { return true }

func pathEqual(a, b string) bool { return a == b }

// realPath 展開所有 symlink 後的絕對路徑。
func realPath(p string) (string, error) { return filepath.EvalSymlinks(filepath.Clean(p)) }
