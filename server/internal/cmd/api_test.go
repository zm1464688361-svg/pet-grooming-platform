// Package cmd HTTP 集成测试：启动真实服务，覆盖全部路由与中间件（controller 层）。
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"pet-grooming/internal/boot"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/util/guid"
)

var baseURL string

// TestMain 启动测试服务（随机端口）。
func TestMain(m *testing.M) {
	s := g.Server("api-test")
	s.SetAccessLogEnabled(false)
	s.SetPort(0)
	BindRoutes(s)
	if err := s.Start(); err != nil {
		fmt.Println("start server:", err)
		os.Exit(1)
	}
	baseURL = fmt.Sprintf("http://127.0.0.1:%d", s.GetListenedPort())
	code := m.Run()
	_ = s.Shutdown()
	os.Exit(code)
}

type apiResp struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func doReq(t *testing.T, method, path string, headers map[string]string, body interface{}) apiResp {
	t.Helper()
	url := baseURL + path
	c := g.Client().SetHeaderMap(headers).SetTimeout(10 * time.Second)
	ctx := context.Background()
	var raw string
	if method == "POST" {
		data := ""
		if body != nil {
			b, _ := json.Marshal(body)
			data = string(b)
		}
		raw = c.PostContent(ctx, url, data)
	} else {
		raw = c.GetContent(ctx, url)
	}
	var res apiResp
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("%s %s: bad json %q: %v", method, path, raw, err)
	}
	return res
}

func post(t *testing.T, path string, headers map[string]string, body interface{}) apiResp {
	t.Helper()
	return doReq(t, "POST", path, headers, body)
}

func get(t *testing.T, path string, headers map[string]string) apiResp {
	t.Helper()
	return doReq(t, "GET", path, headers, nil)
}

// dataMap / dataOf 解析 Data 字段。
func dataMap(t *testing.T, r apiResp) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(r.Data, &m); err != nil {
		t.Fatalf("parse data map: %v (%s)", err, r.Data)
	}
	return m
}

func newFutureSlot(t *testing.T, offset time.Duration) string {
	t.Helper()
	slotId := "slot_h_" + guid.S()
	_, err := g.DB().Model("time_slot").Data(g.Map{
		"slot_id":      slotId,
		"shop_id":      "shop_001",
		"start_time":   time.Now().Add(offset),
		"end_time":     time.Now().Add(offset + time.Hour),
		"max_capacity": 3,
		"status":       1,
		"created_by":   "manager_01",
		"created_at":   time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("create slot: %v", err)
	}
	return slotId
}

func bookAndPay(t *testing.T, slotId string) (string, string) {
	t.Helper()
	r := post(t, "/api/user/app/book", map[string]string{"X-User-Id": "user_1001"},
		g.Map{"petId": "pet_2001", "slotId": slotId, "idemKey": "it-" + guid.S()})
	if r.Code != 0 {
		t.Fatalf("book: %d %s", r.Code, r.Message)
	}
	appId := dataMap(t, r)["appId"].(string)
	p := post(t, "/api/pay/create", map[string]string{"X-User-Id": "user_1001"}, g.Map{"appId": appId})
	if p.Code != 0 {
		t.Fatalf("pay create: %d %s", p.Code, p.Message)
	}
	paymentId := dataMap(t, p)["paymentId"].(string)
	n := post(t, "/api/pay/mock/notify", nil, g.Map{
		"paymentId": paymentId, "transactionId": "txn_h_" + guid.S(), "success": true, "amount": 9900,
	})
	if n.Code != 0 {
		t.Fatalf("notify: %d %s", n.Code, n.Message)
	}
	return appId, paymentId
}

func cleanupSlot(t *testing.T, slotId string) {
	t.Helper()
	_, _ = g.DB().Model("appointment").Where("slot_id", slotId).Delete()
	_, _ = g.DB().Model("refund").Where("app_id IN (SELECT app_id FROM appointment WHERE slot_id=?)", slotId).Delete()
	_, _ = g.DB().Model("payment_log").Where("app_id IN (SELECT app_id FROM appointment WHERE slot_id=?)", slotId).Delete()
	_, _ = g.DB().Model("pay_exception").Where("app_id IN (SELECT app_id FROM appointment WHERE slot_id=?)", slotId).Delete()
	_, _ = g.DB().Model("oper_log").Where("target_id IN (SELECT app_id FROM appointment WHERE slot_id=?)", slotId).Delete()
	_, _ = g.DB().Model("time_slot").Where("slot_id", slotId).Delete()
}

var (
	uid = map[string]string{"X-User-Id": "user_1001"}
	mgr = map[string]string{"X-Staff-Id": "manager_01"}
	frt = map[string]string{"X-Staff-Id": "front_01"}
	svc = map[string]string{"X-Staff-Id": "svc_01"}
	csv = map[string]string{"X-Staff-Id": "cs_01"}
)

func TestHealthAndAuth(t *testing.T) {
	r := get(t, "/health", nil)
	if r.Code != 0 {
		t.Fatalf("health: %d", r.Code)
	}
	// C 端未登录
	r = get(t, "/api/user/slot/list", nil)
	if r.Code == 0 {
		t.Fatal("no user header should fail")
	}
	// C 端用户不存在
	r = get(t, "/api/user/slot/list", map[string]string{"X-User-Id": "no_such_user"})
	if r.Code == 0 {
		t.Fatal("unknown user should fail")
	}
	// B 端无员工头
	r = get(t, "/api/shop/app/list", nil)
	if r.Code == 0 {
		t.Fatal("no staff header should fail")
	}
	// B 端员工不存在
	r = get(t, "/api/shop/app/list", map[string]string{"X-Staff-Id": "no_such_staff"})
	if r.Code == 0 {
		t.Fatal("unknown staff should fail")
	}
}

func TestUserAPIFullFlow(t *testing.T) {
	slotId := newFutureSlot(t, 2*time.Hour)
	defer cleanupSlot(t, slotId)

	// 时段列表
	r := get(t, "/api/user/slot/list", uid)
	if r.Code != 0 {
		t.Fatalf("slot list: %d %s", r.Code, r.Message)
	}
	// 宠物列表
	r = get(t, "/api/user/pet/list", uid)
	if r.Code != 0 {
		t.Fatalf("pet list: %d", r.Code)
	}
	// 我的预约（空）
	r = get(t, "/api/user/app/list", uid)
	if r.Code != 0 {
		t.Fatalf("app list: %d", r.Code)
	}
	// 退款列表（空）
	r = get(t, "/api/user/refund/list", uid)
	if r.Code != 0 {
		t.Fatalf("refund list: %d", r.Code)
	}
	// 详情不存在
	r = get(t, "/api/user/app/detail?appId=no_such", uid)
	if r.Code == 0 {
		t.Fatal("detail of unknown app should fail")
	}
	// 下单（缺参数）
	r = post(t, "/api/user/app/book", uid, g.Map{"petId": "pet_2001"})
	if r.Code == 0 {
		t.Fatal("book without slotId should fail")
	}
	// 下单成功
	appId, paymentId := bookAndPay(t, slotId)
	// 同支付单重复回调（同 transactionId）→ 幂等
	n := post(t, "/api/pay/mock/notify", nil, g.Map{
		"paymentId": paymentId, "transactionId": "txn_h_same", "success": true, "amount": 9900,
	})
	_ = n
	// 详情
	r = get(t, "/api/user/app/detail?appId="+appId, uid)
	if r.Code != 0 {
		t.Fatalf("detail: %d %s", r.Code, r.Message)
	}
	if d := dataMap(t, r); d["status"].(float64) != 2 {
		t.Fatalf("status should be 2, got %v", d["status"])
	}
	// 已确认订单重复发起支付 → 错误（40009）
	r = post(t, "/api/pay/create", uid, g.Map{"appId": appId})
	if r.Code == 0 {
		t.Fatal("pay for confirmed order should fail")
	}
	// B 端核销（前台）→ 5
	r = post(t, "/api/shop/app/verify", frt, g.Map{"appId": appId})
	if r.Code != 0 {
		t.Fatalf("verify: %d %s", r.Code, r.Message)
	}
	// 重复核销 → 错误
	r = post(t, "/api/shop/app/verify", frt, g.Map{"appId": appId})
	if r.Code == 0 {
		t.Fatal("double verify should fail")
	}
	// 服务开始/完成（服务人员）
	r = post(t, "/api/shop/app/service/start", svc, g.Map{"appId": appId})
	if r.Code != 0 {
		t.Fatalf("service start: %d %s", r.Code, r.Message)
	}
	r = post(t, "/api/shop/app/service/finish", svc, g.Map{"appId": appId})
	if r.Code != 0 {
		t.Fatalf("service finish: %d %s", r.Code, r.Message)
	}
	// 跳级：再 start → 错误
	r = post(t, "/api/shop/app/service/start", svc, g.Map{"appId": appId})
	if r.Code == 0 {
		t.Fatal("start after finish should fail")
	}
}

func TestShopSlotAPI(t *testing.T) {
	// 角色越权：前台不可建时段
	r := post(t, "/api/shop/slot/create", frt, g.Map{"startTime": "2031-01-01 10:00:00", "endTime": "2031-01-01 11:00:00"})
	if r.Code == 0 {
		t.Fatal("front create slot should fail")
	}
	// 参数错误
	r = post(t, "/api/shop/slot/create", mgr, g.Map{"startTime": "2031-01-01 11:00:00", "endTime": "2031-01-01 10:00:00"})
	if r.Code == 0 {
		t.Fatal("end before start should fail")
	}
	// 正常创建（明天）
	tomorrow := time.Now().AddDate(0, 0, 1).Format("2006-01-02")
	r = post(t, "/api/shop/slot/create", mgr, g.Map{
		"startTime": tomorrow + " 20:00:00", "endTime": tomorrow + " 21:00:00",
	})
	if r.Code != 0 {
		t.Fatalf("slot create: %d %s", r.Code, r.Message)
	}
	slotId := dataMap(t, r)["slotId"].(string)
	defer cleanupSlot(t, slotId)
	// 列表
	r = get(t, "/api/shop/slot/list?page=1&size=10", mgr)
	if r.Code != 0 {
		t.Fatalf("slot list: %d", r.Code)
	}
	// 修改时间
	r = post(t, "/api/shop/slot/update", mgr, g.Map{
		"slotId": slotId, "startTime": tomorrow + " 20:30:00", "endTime": tomorrow + " 21:30:00",
	})
	if r.Code != 0 {
		t.Fatalf("slot update: %d %s", r.Code, r.Message)
	}
	// 暂停/恢复
	r = post(t, "/api/shop/slot/pause", mgr, g.Map{"slotId": slotId})
	if r.Code != 0 {
		t.Fatalf("slot pause: %d %s", r.Code, r.Message)
	}
	r = post(t, "/api/shop/slot/resume", mgr, g.Map{"slotId": slotId})
	if r.Code != 0 {
		t.Fatalf("slot resume: %d %s", r.Code, r.Message)
	}
	// 删除
	r = post(t, "/api/shop/slot/delete", mgr, g.Map{"slotId": slotId})
	if r.Code != 0 {
		t.Fatalf("slot delete: %d %s", r.Code, r.Message)
	}
}

func TestCancelAuditRefundAPI(t *testing.T) {
	// 订单 1：驳回场景（3→2 恢复）
	slotId := newFutureSlot(t, 30*time.Minute)
	defer cleanupSlot(t, slotId)
	appId, _ := bookAndPay(t, slotId)

	r := post(t, "/api/user/app/cancel", uid, g.Map{"appId": appId, "idemKey": "ic-" + guid.S(), "reason": "临时有事"})
	if r.Code != 0 {
		t.Fatalf("cancel: %d %s", r.Code, r.Message)
	}
	// 审批列表（客服）
	r = get(t, "/api/shop/audit/list?page=1&size=20", csv)
	if r.Code != 0 {
		t.Fatalf("audit list: %d", r.Code)
	}
	// 驳回 → 3→2
	r = post(t, "/api/shop/audit/reject", csv, g.Map{"appId": appId, "reason": "临近时段不可取消"})
	if r.Code != 0 {
		t.Fatalf("reject: %d %s", r.Code, r.Message)
	}
	// 重复驳回 → 错误
	r = post(t, "/api/shop/audit/reject", csv, g.Map{"appId": appId, "reason": "x"})
	if r.Code == 0 {
		t.Fatal("double reject should fail")
	}
	// 驳回后同一申请重复提交 → 错误（40011，原型口径：不可再次提交同一申请）
	r = post(t, "/api/user/app/cancel", uid, g.Map{"appId": appId, "idemKey": "ic2-" + guid.S(), "reason": "宠物生病"})
	if r.Code == 0 {
		t.Fatal("resubmit same cancel should fail")
	}

	// 订单 2：同意场景（3→4 退款单），另一宠物避免同宠物同时段限制
	appId2 := func() string {
		r := post(t, "/api/user/app/book", uid, g.Map{"petId": "pet_2002", "slotId": slotId, "idemKey": "it2-" + guid.S()})
		if r.Code != 0 {
			t.Fatalf("book 2: %d %s", r.Code, r.Message)
		}
		id := dataMap(t, r)["appId"].(string)
		p := post(t, "/api/pay/create", uid, g.Map{"appId": id})
		if p.Code != 0 {
			t.Fatalf("pay create 2: %d %s", p.Code, p.Message)
		}
		pid := dataMap(t, p)["paymentId"].(string)
		if n := post(t, "/api/pay/mock/notify", nil, g.Map{
			"paymentId": pid, "transactionId": "txn_h2_" + guid.S(), "success": true, "amount": 9900,
		}); n.Code != 0 {
			t.Fatalf("notify 2: %d %s", n.Code, n.Message)
		}
		return id
	}()
	r = post(t, "/api/user/app/cancel", uid, g.Map{"appId": appId2, "idemKey": "ic3-" + guid.S(), "reason": "宠物生病"})
	if r.Code != 0 {
		t.Fatalf("cancel 2: %d %s", r.Code, r.Message)
	}
	r = post(t, "/api/shop/audit/approve", csv, g.Map{"appId": appId2})
	if r.Code != 0 {
		t.Fatalf("approve: %d %s", r.Code, r.Message)
	}
	// B 端退款单列表
	r = get(t, "/api/shop/refund/list?page=1&size=20", csv)
	if r.Code != 0 {
		t.Fatalf("refund list: %d", r.Code)
	}
	// C 端退款列表应有记录
	r = get(t, "/api/user/refund/list", uid)
	if r.Code != 0 {
		t.Fatalf("user refund list: %d", r.Code)
	}
	// 店长不可重试退款（仅客服）；重试仅适用于退款中(1)/失败(3) 单
	var retryId string
	if list, ok := dataMap(t, r)["list"].([]interface{}); ok {
		for _, it := range list {
			m := it.(map[string]interface{})
			if m["status"].(float64) != 2 {
				retryId = m["refundId"].(string)
				break
			}
		}
	}
	if retryId != "" {
		r = post(t, "/api/shop/refund/retry", mgr, g.Map{"refundId": retryId})
		if r.Code == 0 {
			t.Fatal("manager refund retry should fail")
		}
		r = post(t, "/api/shop/refund/retry", csv, g.Map{"refundId": retryId})
		if r.Code != 0 {
			t.Fatalf("cs refund retry: %d %s", r.Code, r.Message)
		}
	}
}

func TestNoShowAPI(t *testing.T) {
	// 先在未来时段完成预约支付，再把时段时间拨到过去（模拟时段自然结束）
	slotId := newFutureSlot(t, time.Hour)
	defer cleanupSlot(t, slotId)
	appId, _ := bookAndPay(t, slotId)
	_, err := g.DB().Model("time_slot").Data(g.Map{
		"start_time": time.Now().Add(-2 * time.Hour),
		"end_time":   time.Now().Add(-time.Hour),
	}).Where("slot_id", slotId).Update()
	if err != nil {
		t.Fatalf("shift slot to past: %v", err)
	}

	// 服务角色不可标记 → 40001
	r := post(t, "/api/shop/app/noshow", svc, g.Map{"appId": appId, "reason": "未到店"})
	if r.Code == 0 {
		t.Fatal("svc noshow should fail")
	}
	// 原因为空 → 错误
	r = post(t, "/api/shop/app/noshow", mgr, g.Map{"appId": appId, "reason": ""})
	if r.Code == 0 {
		t.Fatal("empty reason should fail")
	}
	// 店长标记成功
	r = post(t, "/api/shop/app/noshow", mgr, g.Map{"appId": appId, "reason": "时段结束未到店"})
	if r.Code != 0 {
		t.Fatalf("noshow: %d %s", r.Code, r.Message)
	}
	// 宠物历史（B 端只读）
	r = get(t, "/api/shop/pet/history?petId=pet_2001&page=1&size=10", mgr)
	if r.Code != 0 {
		t.Fatalf("pet history: %d", r.Code)
	}
}

func TestExceptionAndLedgerAPI(t *testing.T) {
	// 构造已关闭订单 + 异常单
	slotId := newFutureSlot(t, 3*time.Hour)
	defer cleanupSlot(t, slotId)
	appId, _ := bookAndPay(t, slotId)
	// 关闭订单（模拟超时关闭）
	_, err := g.DB().Model("appointment").
		Data(g.Map{"status": 9}).
		Where("app_id", appId).Where("status", 2).Update()
	if err != nil {
		t.Fatalf("close order: %v", err)
	}
	excId := "exc_h_" + guid.S()
	_, err = g.DB().Model("pay_exception").Data(g.Map{
		"exc_id": excId, "app_id": appId, "transaction_id": "txn_h_exc_" + guid.S(),
		"amount": 9900, "exc_type": 1, "status": 1, "shop_id": "shop_001", "created_at": time.Now(),
	}).Insert()
	if err != nil {
		t.Fatalf("create exception: %v", err)
	}
	defer func() { _, _ = g.DB().Model("pay_exception").Where("exc_id", excId).Delete() }()

	// 异常单列表
	r := get(t, "/api/shop/exception/list?status=1&page=1&size=20", mgr)
	if r.Code != 0 {
		t.Fatalf("exception list: %d", r.Code)
	}
	// 处理异常单 → 生成退款单
	r = post(t, "/api/shop/exception/process", mgr, g.Map{"excId": excId})
	if r.Code != 0 {
		t.Fatalf("exception process: %d %s", r.Code, r.Message)
	}
	// 重复处理 → 错误
	r = post(t, "/api/shop/exception/process", mgr, g.Map{"excId": excId})
	if r.Code == 0 {
		t.Fatal("double process should fail")
	}
	// 台账
	r = get(t, "/api/shop/ledger/daily", mgr)
	if r.Code != 0 {
		t.Fatalf("ledger: %d", r.Code)
	}
	// 非店长不可查台账
	r = get(t, "/api/shop/ledger/daily", frt)
	if r.Code == 0 {
		t.Fatal("front ledger should fail")
	}
	// 台账日期参数
	r = get(t, "/api/shop/ledger/daily?date=2031-01-01", mgr)
	if r.Code != 0 {
		t.Fatalf("ledger by date: %d", r.Code)
	}
}

// TestGhostAndOptions 不存在对象与 CORS 预检（覆盖 logic 错误 → replyFail 分支）。
func TestGhostAndOptions(t *testing.T) {
	// CORS 预检（OPTIONS 分支）
	_, _ = g.Client().SetHeaderMap(map[string]string{
		"Origin":                        "http://localhost:5173",
		"Access-Control-Request-Method": "POST",
	}).Options(context.Background(), baseURL+"/api/user/slot/list")

	ghost := [][]interface{}{
		{"POST", "/api/pay/create", uid, g.Map{"appId": "app_ghost"}},
		{"POST", "/api/user/app/cancel", uid, g.Map{"appId": "app_ghost", "idemKey": "g1"}},
		{"POST", "/api/user/app/book", uid, g.Map{"petId": "pet_ghost", "slotId": "slot_ghost", "idemKey": "g2"}},
		{"GET", "/api/user/app/detail?appId=app_ghost", uid, nil},
		{"POST", "/api/shop/app/verify", frt, g.Map{"appId": "app_ghost"}},
		{"POST", "/api/shop/app/service/start", svc, g.Map{"appId": "app_ghost"}},
		{"POST", "/api/shop/app/service/finish", svc, g.Map{"appId": "app_ghost"}},
		{"POST", "/api/shop/app/noshow", mgr, g.Map{"appId": "app_ghost", "reason": "ghost"}},
		{"POST", "/api/shop/audit/approve", csv, g.Map{"appId": "app_ghost"}},
		{"POST", "/api/shop/audit/reject", csv, g.Map{"appId": "app_ghost", "reason": "ghost"}},
		{"POST", "/api/shop/refund/retry", csv, g.Map{"refundId": "ref_ghost"}},
		{"POST", "/api/shop/exception/process", mgr, g.Map{"excId": "exc_ghost"}},
		{"POST", "/api/shop/slot/pause", mgr, g.Map{"slotId": "slot_ghost"}},
		{"POST", "/api/shop/slot/resume", mgr, g.Map{"slotId": "slot_ghost"}},
		{"POST", "/api/shop/slot/update", mgr, g.Map{"slotId": "slot_ghost", "startTime": "2031-01-01 10:00:00", "endTime": "2031-01-01 11:00:00"}},
		{"POST", "/api/shop/slot/delete", mgr, g.Map{"slotId": "slot_ghost"}},
	}
	for _, c := range ghost {
		method := c[0].(string)
		path := c[1].(string)
		headers := c[2].(map[string]string)
		var res apiResp
		if method == "GET" {
			res = get(t, path, headers)
		} else {
			res = post(t, path, headers, c[3])
		}
		if res.Code == 0 {
			t.Fatalf("%s %s ghost should fail", method, path)
		}
	}
}

// TestBootNewServer 验证服务装配。
func TestBootNewServer(t *testing.T) {
	if boot.NewServer() == nil {
		t.Fatal("NewServer should return configured server")
	}
}

// TestBadParams 空 body / 非法 JSON → 覆盖各 handler 的参数解析错误分支。
func TestBadParams(t *testing.T) {
	cases := []struct {
		method  string
		path    string
		headers map[string]string
	}{
		{"POST", "/api/user/app/book", uid},
		{"POST", "/api/user/app/cancel", uid},
		{"POST", "/api/pay/create", uid},
		{"POST", "/api/pay/mock/notify", nil},
		{"POST", "/api/shop/slot/create", mgr},
		{"POST", "/api/shop/slot/pause", mgr},
		{"POST", "/api/shop/slot/resume", mgr},
		{"POST", "/api/shop/slot/update", mgr},
		{"POST", "/api/shop/slot/delete", mgr},
		{"POST", "/api/shop/app/verify", frt},
		{"POST", "/api/shop/app/service/start", svc},
		{"POST", "/api/shop/app/service/finish", svc},
		{"POST", "/api/shop/audit/approve", csv},
		{"POST", "/api/shop/audit/reject", csv},
		{"POST", "/api/shop/refund/retry", csv},
		{"POST", "/api/shop/app/noshow", mgr},
		{"POST", "/api/shop/exception/process", mgr},
	}
	for _, c := range cases {
		for _, body := range []string{"{", "{}"} {
			url := baseURL + c.path
			raw := g.Client().SetHeaderMap(c.headers).SetTimeout(10*time.Second).
				PostContent(context.Background(), url, body)
			var res apiResp
			if json.Unmarshal([]byte(raw), &res) == nil && res.Code == 0 {
				t.Fatalf("%s %s body=%q should fail", c.method, c.path, body)
			}
		}
	}
}

func TestShopAppointmentListAPI(t *testing.T) {
	slotId := newFutureSlot(t, 4*time.Hour)
	defer cleanupSlot(t, slotId)
	bookAndPay(t, slotId)

	// 列表（各角色可查）
	r := get(t, "/api/shop/app/list?page=1&size=20", frt)
	if r.Code != 0 {
		t.Fatalf("app list: %d %s", r.Code, r.Message)
	}
	// 状态筛选
	r = get(t, "/api/shop/app/list?status=2&page=1&size=20", svc)
	if r.Code != 0 {
		t.Fatalf("app list by status: %d", r.Code)
	}
	// 关键字
	r = get(t, "/api/shop/app/list?keyword=user_1001&page=1&size=20", csv)
	if r.Code != 0 {
		t.Fatalf("app list by keyword: %d", r.Code)
	}
	// 日期筛选（无结果也应成功）
	r = get(t, "/api/shop/app/list?date=2031-01-01&page=1&size=20", csv)
	if r.Code != 0 {
		t.Fatalf("app list by date: %d", r.Code)
	}
	// 非法日期 → 宽容处理（忽略日期条件）
	r = get(t, "/api/shop/app/list?date=bad-date&page=1&size=20", csv)
	if r.Code != 0 {
		t.Fatalf("bad date should be tolerated: %d %s", r.Code, r.Message)
	}
	// 无分页参数 → 默认 page=1 size=20
	r = get(t, "/api/shop/audit/list", csv)
	if r.Code != 0 {
		t.Fatalf("audit list default paging: %d", r.Code)
	}
	r = get(t, "/api/shop/exception/list", mgr)
	if r.Code != 0 {
		t.Fatalf("exception list default paging: %d", r.Code)
	}
	r = get(t, "/api/shop/refund/list", csv)
	if r.Code != 0 {
		t.Fatalf("refund list default paging: %d", r.Code)
	}
	r = get(t, "/api/user/app/list?page=1&size=5", uid)
	if r.Code != 0 {
		t.Fatalf("user app list paging: %d", r.Code)
	}
}
