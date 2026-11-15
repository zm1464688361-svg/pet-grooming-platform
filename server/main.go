package main

import (
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gctx"

	_ "pet-grooming/internal/boot"
	"pet-grooming/internal/cmd"
	payCtl "pet-grooming/internal/controller/pay"
	userCtl "pet-grooming/internal/controller/user"
	"pet-grooming/internal/logic/timer"
)

func main() {
	s := g.Server()
	s.SetPort(8000)
	s.SetIndexFolder(false)
	s.AddStaticPath("/public", "public")

	s.Group("/", func(group *ghttp.RouterGroup) {
		group.GET("/health", cmd.Health)
		group.Middleware(cmd.CORS)
		ctl := userCtl.New()
		group.Group("/api/user", func(group *ghttp.RouterGroup) {
			group.Middleware(cmd.UserIdAuth)
			group.GET("/slot/list", ctl.SlotList) // 可约时段与服务项目
			group.GET("/pet/list", ctl.PetList)   // 用户宠物
			group.POST("/app/book", ctl.Book)     // 预约下单
			// 其余 C 端路由在各 Task 中逐步注册
		})
		group.Group("/api/shop", func(group *ghttp.RouterGroup) {
			group.Middleware(cmd.StaffAuth)
			// B 端路由在各 Task 中逐步注册
		})
		payAPI := payCtl.New()
		group.Group("/api/pay", func(group *ghttp.RouterGroup) {
			group.POST("/mock/notify", payAPI.MockNotify) // 模拟渠道回调（无登录态）
			payAuth := group.Group("")
			payAuth.Middleware(cmd.UserIdAuth)
			payAuth.POST("/create", payAPI.Create) // 发起支付（C 端登录态）
		})
	})

	// 定时任务：待支付订单超时关闭（每 30 秒扫描）
	timer.Start(gctx.New())

	s.Run()
}
