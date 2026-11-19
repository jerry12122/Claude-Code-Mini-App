package server

import (
	"context"
	"errors"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/config"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
)

// StartIsolated 給臨時程序。它走與正式啟動相同的路由、30 秒輪詢與內部 WS，
// 但只聽 127.0.0.1，不同步模型、不暖機，額度只來自 refresh。正式 Start 仍聽所有介面，不會進這裡。
func StartIsolated(ctx context.Context, refresh func(context.Context, string) (quota.Snapshot, error), seedProvider string, seed quota.Snapshot) (*Server, error) {
	if refresh == nil {
		return nil, errors.New("隔離啟動需要額度刷新")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return runNormal(ctx, cfg, isolatedExternal{refresh: refresh, seedProvider: seedProvider, seed: seed})
}
