// Package cmd 路由绑定（main 与 HTTP 集成测试共用）。
package cmd

import (
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/gogf/gf/v2/net/ghttp"

	payCtl "pet-grooming/internal/controller/pay"
	shopCtl "pet-grooming/internal/controller/shop"
	userCtl "pet-grooming/internal/controller/user"
)

// UIStatic 前端静态资源（带禁缓存头，页面更新即时生效）。
func UIStatic(r *ghttp.Request) {
	path := r.Get("path").String()
	if path == "" || strings.HasSuffix(path, "/") {
		path += "home.html"
	}
	fp := filepath.Join("manifest/static", filepath.Clean("/"+path))
	data, err := os.ReadFile(fp)
	if err != nil {
		r.Response.WriteStatus(404)
		return
	}
	r.Response.Header().Set("Cache-Control", "no-store, must-revalidate")
	r.Response.Header().Set("Content-Type", mime.TypeByExtension(filepath.Ext(fp)))
	r.Response.WriteHeader(200)
	r.Response.Write(data)
}

// BindRoutes 注册全部 API 路由与中间件。
func BindRoutes(s *ghttp.Server) {
	s.Group("/", func(group *ghttp.RouterGroup) {
		group.GET("/health", Health)
		group.GET("/ui/*path", UIStatic) // 前端页面（禁缓存）
		group.Middleware(CORS)
		ctl := userCtl.New()
		group.Group("/api/user", func(group *ghttp.RouterGroup) {
			group.Middleware(UserIdAuth)
			group.GET("/slot/list", ctl.SlotList)     // 可约时段与服务项目
			group.GET("/pet/list", ctl.PetList)       // 用户宠物
			group.GET("/app/list", ctl.MyAppList)     // 我的预约
			group.GET("/app/detail", ctl.MyAppDetail) // 预约详情
			group.GET("/refund/list", ctl.MyRefunds)  // 我的退款单
			group.POST("/app/book", ctl.Book)         // 预约下单
			group.POST("/app/cancel", ctl.Cancel)     // 取消预约
		})
		group.Group("/api/shop", func(group *ghttp.RouterGroup) {
			group.Middleware(StaffAuth)
			shopAPI := shopCtl.New()
			group.GET("/slot/list", shopAPI.SlotList)                  // 时段列表（分页）
			group.POST("/slot/create", shopAPI.SlotCreate)             // 发布时段（店长）
			group.POST("/slot/pause", shopAPI.SlotPause)               // 暂停时段（店长）
			group.POST("/slot/resume", shopAPI.SlotResume)             // 恢复时段（店长）
			group.POST("/slot/update", shopAPI.SlotUpdate)             // 修改时段（店长）
			group.POST("/slot/delete", shopAPI.SlotDelete)             // 删除时段（店长）
			group.GET("/app/list", shopAPI.AppointmentList)            // 预约查询（全角色）
			group.GET("/pet/history", shopAPI.PetHistory)              // 宠物历史（只读）
			group.POST("/app/verify", shopAPI.AppVerify)               // 到店核销（前台/店长）
			group.POST("/app/service/start", shopAPI.ServiceStart)     // 登记开始（服务/店长）
			group.POST("/app/service/finish", shopAPI.ServiceFinish)   // 登记完成（服务/店长）
			group.GET("/audit/list", shopAPI.AuditList)                // 取消审批列表（客服/店长）
			group.POST("/audit/approve", shopAPI.AuditApprove)         // 审批同意（客服/店长）
			group.POST("/audit/reject", shopAPI.AuditReject)           // 审批驳回（客服/店长）
			group.POST("/refund/retry", shopAPI.RefundRetry)           // 退款重试（仅客服）
			group.POST("/app/noshow", shopAPI.AppNoShow)               // 标记未到店（店长）
			group.GET("/exception/list", shopAPI.ExceptionList)        // 支付异常单列表（店长/客服）
			group.POST("/exception/process", shopAPI.ExceptionProcess) // 处理异常单（店长/客服）
			group.GET("/ledger/daily", shopAPI.LedgerDaily)            // 日清台账（店长）
			group.GET("/refund/list", shopAPI.RefundAdminList)         // 退款单列表（客服/店长）
		})
		payAPI := payCtl.New()
		group.Group("/api/pay", func(group *ghttp.RouterGroup) {
			group.POST("/mock/notify", payAPI.MockNotify) // 模拟渠道回调（无登录态）
			payAuth := group.Group("")
			payAuth.Middleware(UserIdAuth)
			payAuth.POST("/create", payAPI.Create) // 发起支付（C 端登录态）
		})
	})
}
