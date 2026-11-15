// Package boot 统一注册驱动与组件（main 与测试共用）。
package boot

import (
	"os"
	"strings"

	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
	_ "github.com/gogf/gf/contrib/nosql/redis/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gctx"
)

func init() {
	// 数据库密码经环境变量注入（占位符 ${MYSQL_PASSWORD}），敏感数据不入库
	ctx := gctx.New()
	v, err := g.Cfg().Get(ctx, "database.default.link")
	if err != nil || v == nil {
		return
	}
	link := v.String()
	if strings.Contains(link, "${MYSQL_PASSWORD}") {
		pw := os.Getenv("MYSQL_PASSWORD")
		if pw == "" {
			panic("MYSQL_PASSWORD 环境变量未设置（参考 server/.env.example）")
		}
		link = strings.ReplaceAll(link, "${MYSQL_PASSWORD}", pw)
	}
	// gdb.Config 为 map 结构：分组名 → ConfigGroup（节点列表）
	node := gdb.ConfigNode{Type: "mysql", Link: link}
	gdb.SetConfig(gdb.Config{
		"default": gdb.ConfigGroup{node},
	})
}

// NewServer 构造并配置 HTTP 服务（端口、静态资源），路由由 cmd.BindRoutes 注册。
func NewServer() *ghttp.Server {
	s := g.Server()
	s.SetPort(8000)
	s.SetIndexFolder(false)
	s.AddStaticPath("/public", "public")
	s.AddStaticPath("/ui", "manifest/static") // 前端页面（原型图实现）
	return s
}
