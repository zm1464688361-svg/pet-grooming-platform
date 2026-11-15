package main

import (
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"

	"pet-grooming/internal/cmd"
)

func main() {
	s := g.Server()
	s.SetPort(8000)
	s.SetIndexFolder(false)
	s.AddStaticPath("/public", "public")

	s.Group("/", func(group *ghttp.RouterGroup) {
		group.GET("/health", cmd.Health)
		group.Middleware(cmd.CORS)
		group.Group("/api/user", func(user *ghttp.RouterGroup) {
			user.Middleware(cmd.UserIdAuth)
			// C 端路由在各 Task 中逐步注册
			_ = user
		})
		group.Group("/api/shop", func(shop *ghttp.RouterGroup) {
			shop.Middleware(cmd.StaffAuth)
			// B 端路由在各 Task 中逐步注册
			_ = shop
		})
		group.Group("/api/pay", func(pay *ghttp.RouterGroup) {
			// 模拟支付渠道与回调路由在各 Task 中注册
			_ = pay
		})
	})

	s.Run()
}
