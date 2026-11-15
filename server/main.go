// Package main 服务入口：注册路由并启动 HTTP 服务。
package main

import (
	"github.com/gogf/gf/v2/os/gctx"

	"pet-grooming/internal/boot"
	"pet-grooming/internal/cmd"
	"pet-grooming/internal/logic/timer"
)

func main() {
	s := boot.NewServer()
	cmd.BindRoutes(s)

	// 定时任务：待支付订单超时关闭（每 30 秒扫描）
	timer.Start(gctx.New())

	s.Run()
}
