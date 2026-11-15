package main

import (
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"

	_ "pet-grooming/internal/boot"
	"pet-grooming/internal/cmd"
	userCtl "pet-grooming/internal/controller/user"
)

func main() {
	s := g.Server()
	s.SetPort(8000)
	s.SetIndexFolder(false)
	s.AddStaticPath("/public", "public")

	s.Group("/", func(group *ghttp.RouterGroup) {
		group.GET("/health", cmd.Health)
		group.Middleware(cmd.CORS)
		group.Group("/api/user", func(group *ghttp.RouterGroup) {
			group.Middleware(cmd.UserIdAuth)
			group.Bind(userCtl.New())
		})
		group.Group("/api/shop", func(group *ghttp.RouterGroup) {
			group.Middleware(cmd.StaffAuth)
			// B 端路由在各 Task 中逐步注册
		})
		group.Group("/api/pay", func(group *ghttp.RouterGroup) {
			// 模拟支付渠道与回调路由在各 Task 中注册
		})
	})

	s.Run()
}
