package main

import (
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gctx"

	_ "pet-grooming/internal/boot"
	"pet-grooming/internal/cmd"
	payCtl "pet-grooming/internal/controller/pay"
	shopCtl "pet-grooming/internal/controller/shop"
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
			group.GET("/slot/list", ctl.SlotList)     // 可约时段与服务项目
			group.GET("/pet/list", ctl.PetList)       // 用户宠物
			group.GET("/app/list", ctl.MyAppList)     // 我的预约
			group.GET("/app/detail", ctl.MyAppDetail) // 预约详情
			group.POST("/app/book", ctl.Book)         // 预约下单
			group.POST("/app/cancel", ctl.Cancel)     // 取消预约
			// 其余 C 端路由在各 Task 中逐步注册
		})
		group.Group("/api/shop", func(group *ghttp.RouterGroup) {
			group.Middleware(cmd.StaffAuth)
			shopAPI := shopCtl.New()
			group.GET("/slot/list", shopAPI.SlotList)                // 时段列表（分页）
			group.POST("/slot/create", shopAPI.SlotCreate)           // 发布时段（店长）
			group.POST("/slot/pause", shopAPI.SlotPause)             // 暂停时段（店长）
			group.POST("/slot/resume", shopAPI.SlotResume)           // 恢复时段（店长）
			group.POST("/slot/update", shopAPI.SlotUpdate)           // 修改时段（店长）
			group.POST("/slot/delete", shopAPI.SlotDelete)           // 删除时段（店长）
			group.GET("/app/list", shopAPI.AppointmentList)          // 预约查询（全角色）
			group.GET("/pet/history", shopAPI.PetHistory)            // 宠物历史（只读）
			group.POST("/app/verify", shopAPI.AppVerify)             // 到店核销（前台/店长）
			group.POST("/app/service/start", shopAPI.ServiceStart)   // 登记开始（服务/店长）
			group.POST("/app/service/finish", shopAPI.ServiceFinish) // 登记完成（服务/店长）
			// B 端其余路由在各 Task 中逐步注册
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
