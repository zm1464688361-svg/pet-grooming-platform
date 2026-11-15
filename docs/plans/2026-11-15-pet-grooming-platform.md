# 题目02 宠物洗护预约平台 实施计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 实现单门店（shop_001）宠物洗护预约 O2O 全链路：店长发布时段 → 宠物主人预约支付 → 前台核销 → 服务登记 → 取消审核/退款 → 店长日清核查。

**Architecture:** 单体 Go 服务（GoFrame v2）+ MySQL（真值源）+ Redis（名额缓存 + Lua 原子占用/释放）+ 模拟支付渠道（进程内 HTTP 接口）。C 端与 B 端共用同一套 appointment 记录与状态机（1~9），所有状态变更走条件更新（CAS）。前端为静态 HTML 页面（按 `原型图/` 16 个原型实现），由 GoFrame 静态目录托管，fetch 调用 REST API。

**Tech Stack:** Go 1.22+ / GoFrame v2 / MySQL 8 / Redis 7 / 原生 HTML+Tailwind+JS（按原型图）

**约束来源（必须遵守，禁止改动业务口径）:**
- `.agent/agentrules.md` — C 端约束（名额时序红线、支付幂等、错误码 40001~40011、文案）
- `.agent/agentrules (1).md` — C 端抢名额补充约束（Lua 脚本、定时任务、支付重复提交）
- `.agent/agentrules-B端.md` — B 端约束（角色权限矩阵、错误码 50001~50010、日清口径、oper_log）
- `.agent/Go项目业务场景题.md` — 考试要求（AGENTS.md 先行、全程 dev/02 分支、提交规范）
- `.agent/commit-convention.md` — 提交格式 `[类型] [版本号] [编号] 描述`
- `原型图/*.html` — 前端页面视觉与交互基准

**核心不变式（写代码时随时对照）:**
1. 名额占用时序：DB 事务提交成功（status=1 订单落库）→ 才调 Redis Lua 占用；禁止先扣后建、禁止支付后扣
2. 名额释放仅 4 场景：待支付超时(1→9)、待支付主动取消、≥2h 自动取消(2→4)、客服审批同意(3→4)；释放集中在单一方法，先 SISMEMBER 再 DECR，负数告警
3. 状态机：1→2→5→6→7；2→4（≥2h）；2→3→4/2（<2h）；2→8（时段结束未核销）；终态不可回退
4. 所有状态变更 `UPDATE ... WHERE status=?`，影响行数判定成败
5. 时间判定全部服务端时间、秒级比较，恰好 2h 按 ≥2h
6. 支付：pay:lock:{appId} SETNX 30s → 40008；仅 status=1 可支付 → 40009；回调幂等（payment_log 交易号唯一键）；同号重推幂等不退款，异号重复扣款退多余；关闭后支付 → 登记异常 + 原路退款 + 状态不变
7. 角色：店长(1)/前台(2)/服务(3)/客服(4)，权限矩阵按 agentrules-B端 §2.1，禁止万能账号
8. 台账四金额独立：已收款 ≠ 已完成服务收入（仅 status=7）

---

## Task 0: AGENTS.md 项目约束文件

**Files:** Create `AGENTS.md`（仓库根目录）

**Step 1:** 按 `.agent/Go项目业务场景题.md` §3 要求编写 AGENTS.md：三个 Agent（开发/测试/验收）职责与交接、分支与提交规范（dev/02、commit-convention）、业务规则引用 .agent 约束文件、状态/名额/时间口径摘要、禁止清单引用、单元测试覆盖率要求。

**Step 2:** 提交：`git add AGENTS.md && git commit -m "[docs] 编写 AGENTS.md 项目约束"` 并推送 dev/02。

## Task 1: 项目脚手架

**Files:** Create `server/go.mod`、`server/main.go`、`server/config/config.yaml`、`server/internal/logic/...` 目录骨架、`server/docker-compose.yml`（MySQL+Redis）、`server/README.md`

**Step 1:** 检查环境：`go version`；若无 Go 则用 install_binary 安装。检查本机 3306/6379 是否可用，不可用则用 docker-compose。
**Step 2:** go mod init + 引入 github.com/gogf/gf/v2；main.go 启动 HTTP 服务、挂载路由分组 `/api/user`、`/api/shop`、`/api/pay`、静态目录 `server/public`。
**Step 3:** config.yaml：mysql/redis 连接、常量（maxCapacity=3、price=9900、lockMinutes=5、cancelWindowHours=2、payLockSeconds=30）。
**Step 4:** 验证：`go build ./...` 通过；`go run main.go` 后 GET /health 返回 ok。
**Step 5:** Commit: `[feat] [v0.1.0] 项目脚手架：GoFrame 服务骨架与配置`

## Task 2: 数据库 Schema 与预置数据

**Files:** Create `server/manifest/sql/schema.sql`、`server/manifest/sql/seed.sql`

**Step 1:** 按 C端文档 §10 + B端文档 §15 建表（字段与文档一致）：
- `shop`（shop_001）、`service_item`（item_wash_001, 9900）
- `user`（预置 3 用户 user_1001/1002/1003，含手机号）、`pet`（预置 5+ 宠物，绑定 user，status 1启用/2停用）
- `time_slot`（slot_id, shop_id, start_time, end_time, max_capacity=3, status 1可预约/2暂停/3进行中/4已结束）
- `appointment`（含 user_remark, shop_remark；UNIQUE uk_pet_slot(pet_id,slot_id)、idx_slot_status）
- `payment`（uk_app）、`payment_log`（uk_txn）、`refund`（refund_type 1/2、status 1/2/3、retry_count、fail_msg）
- `shop_staff`（4 角色）、`oper_log`、`pay_exception`
**Step 2:** seed.sql：shop_001、员工（manager_01/前台 front_01/服务 svc_01/客服 cs_01）、用户与宠物、3 个未来时段（相对 NOW() 生成：+3h、明天+2h、后天）。
**Step 3:** 验证：mysql client 执行后 `SELECT COUNT(*) FROM time_slot` = 3。
**Step 4:** Commit: `[feat] [v0.1.0] 数据库表结构与预置数据脚本`

## Task 3: 常量、错误码、通用响应与认证

**Files:** Create `server/internal/consts/`（status.go/error.go/role.go）、`server/internal/controller/` 骨架、`server/internal/logic/middleware/auth.go`

**Step 1:** 状态常量 StatusPending=1...StatusTimeoutClosed=9；错误码 C 端 40001~40011、B 端 50001~50010，文案按约束文件。
**Step 2:** 认证采用简化登录态：C 端请求头 `X-User-Id`；B 端 `X-Staff-Id`（middleware 查 user/shop_staff 得到身份与角色，写入 Context）。C 端所有查询强制带 user_id 条件；B 端按 staff.shop_id 过滤。
**Step 3:** 验证：单测身份解析；`go test ./internal/...`。
**Step 4:** Commit: `[feat] [v0.1.0] 状态常量、错误码与认证中间件`

## Task 4: Redis Lua 名额占用/释放 + 对账

**Files:** Create `server/internal/logic/quota/`（lua 脚本嵌入 go:embed 或常量字符串）、`server/internal/dao/`

**Step 1:** 占用脚本（SISMEMBER→容量判定→INCR+SADD，返回 {1,OK}/{0,PET_DUPLICATED}/{0,QUOTA_FULL}）；释放脚本（SISMEMBER→DECR+SREM，NOT_OCCUPIED 防重复）；统一释放入口 `ReleaseQuota(slotId, petId)`，唯一调用点。
**Step 2:** 对账方法 `RebuildQuota(slotId)`：以 DB 有效预约(1,2,3)重建计数与集合，单向以库为准。
**Step 3:** 测试（需本地 Redis）：并发 10 goroutine 抢 3 名额，仅 3 成功；重复释放不产生负数。
**Step 4:** Commit: `[feat] [v0.1.0] Redis Lua 名额占用与释放`

## Task 5: C 端时段列表与宠物列表

**Files:** Create `server/internal/controller/user/slot.go`、`server/internal/logic/user/slot.go`

**Step 1:** GET `/api/user/slot/list`：三重过滤（shop 匹配 + status=1 + start_time > now），返回剩余名额、是否可约、是否已满（服务端计算）。
**Step 2:** GET `/api/user/pet/list`：当前 user_id 名下启用宠物（user_id 强制条件）。
**Step 3:** 测试：停用宠物不返回；已开始时段不返回。
**Step 4:** Commit: `[feat] [v0.1.0] C端时段与宠物查询接口`

## Task 6: C 端预约下单（时序红线）

**Files:** Create `server/internal/logic/user/booking.go`、`server/internal/controller/user/appointment.go`

**Step 1:** POST `/api/user/app/book`（幂等键 book:idem:{userId}:{key} 10min）：前置校验链（宠物归属→时段可约→未开始→名额→宠物唯一）→ DB 事务（宠物唯一校验、容量校验、INSERT status=1, lock_expire_at=now+5min, uk_pet_slot 兜底）→ **事务提交成功后**调 Lua 占用（失败仅告警不回滚）→ 返回文案「预约成功，名额保留5分钟，请尽快完成支付」。
**Step 2:** 测试：并发 4 用户抢最后 1 名额仅 1 成功（C-02）；同宠物重复预约 40004（C-13）；时段已开始 40002（C-14）；无权宠物 40001。
**Step 3:** Commit: `[feat] [v0.1.0] C端预约下单与名额占用时序`

## Task 7: 定时任务超时关闭

**Files:** Create `server/internal/logic/task/timeout.go`（gcron 每分钟）

**Step 1:** 扫描 `status=1 AND lock_expire_at<NOW()` → 条件更新 1→9（带 status=1 条件）→ 影响行数=1 才调释放 → 写日志。
**Step 2:** 测试：构造过期单，任务执行后状态 9、名额释放、二次扫描不再释放（C-03）。
**Step 3:** Commit: `[feat] [v0.1.0] 待支付订单超时关闭定时任务`

## Task 8: 模拟支付渠道 + 支付发起 + 回调

**Files:** Create `server/internal/logic/pay/channel_mock.go`、`server/internal/logic/pay/pay.go`、`server/internal/controller/pay/callback.go`

**Step 1:** 模拟渠道（`/api/pay/mock/*`）：统一下单返回支付凭证；可配置扣款成功/失败、重复扣款（两笔不同 transaction_id）、退款成功/失败（演示用开关，如请求参数 simulate）。
**Step 2:** POST `/api/user/app/pay`：归属校验→仅 status=1（40009）→ SETNX pay:lock:{appId} 30s 失败 40008 → defer Del → out_trade_no=appId。前端配合按钮置灰。
**Step 3:** POST `/api/pay/callback`：验签(简化)+金额=9900 → 先写 payment_log（uk_txn，先落库者为首笔）→ status=1 确认为 2、清空 lock_expire_at；status≠1：同号重推幂等返回成功；异号且订单已关闭(9/4) → 登记 pay_exception + refund_type=2 全额退款单 + 原路退款，状态不变；异号且订单已确认 → 重复扣款退款，状态不变、名额不释放。
**Step 4:** 测试：C-11 重复推送 3 次仅 1 条流水；C-12/C-13 关闭后支付登记异常不恢复；C-16/C-19 并发双击仅一笔预支付；C-18 两笔异号仅首笔确认。
**Step 5:** Commit: `[feat] [v0.1.0] 模拟支付渠道、支付发起幂等与回调处理`

## Task 9: C 端取消申请与退款

**Files:** Create `server/internal/logic/user/cancel.go`、`server/internal/logic/refund/refund.go`

**Step 1:** POST `/api/user/app/cancel-apply`：仅 status=1（→主动取消，释放名额）或 status=2；剩余 ≥2h（秒级，恰好 2h 按 ≥2h）→ CAS 2→4 + 生成 refund_type=1 全额退款单 + 释放名额；<2h → 原因必填（40010）→ CAS 2→3（40006/40011 拦截）；状态 5/6/7/8 拒绝。
**Step 2:** 退款执行：模拟渠道退款；成功→refund.status=2；失败→status=3 记 fail_msg；退款失败不改预约、不释放名额。
**Step 3:** 测试：C-05 ≥2h 自动受理；C-20 恰好 2h 走自动；C-06/C-07 <2h 分支；C-21 重复提交 40011；C-08 退款失败预约状态不变。
**Step 4:** Commit: `[feat] [v0.1.0] C端取消申请分流与退款子流程`

## Task 10: C 端查询接口

**Files:** Create `server/internal/controller/user/query.go`

**Step 1:** GET `/api/user/app/list`（按状态分组，仅本人）、GET `/api/user/app/detail`（状态文案映射、按钮显隐、精简日志、refund 记录、noshow 原因）、GET `/api/user/app/refund-detail`、POST `/api/user/app/sync-status`（渠道查询补偿：复用回调幂等逻辑，已关闭晚到支付走异常退款，C-22）。
**Step 2:** Commit: `[feat] [v0.1.0] C端预约列表详情退款查询与状态同步`

## Task 11: B 端身份与时段管理

**Files:** Create `server/internal/logic/shop/slot.go`

**Step 1:** POST `/api/shop/slot/create`（店长）：结束>开始、开始>now、同店不重叠、容量强制 3；写 oper_log。
**Step 2:** POST `/api/shop/slot/pause|resume|update|delete`：存在有效预约(1,2,3)一律 50002；写日志。
**Step 3:** GET `/api/shop/slot/list`（店长/前台）：按日期筛选，容量只读。
**Step 4:** 测试：B-01/B-02。
**Step 5:** Commit: `[feat] [v0.1.0] B端时段管理`

## Task 12: B 端预约查询与宠物历史

**Files:** Create `server/internal/logic/shop/appointment.go`

**Step 1:** GET `/api/shop/app/list`：强制分页（LIMIT/OFFSET，默认 20）、创建时间倒序、shop_id 隔离、日期/状态/手机号/宠物名模糊检索；C 端接口永不返回 shop_remark。
**Step 2:** GET `/api/shop/app/detail`：含 user_remark/shop_remark、取消原因、退款记录、异常记录。
**Step 3:** GET `/api/shop/app/pet-history`：只读本店该宠物历史。
**Step 4:** Commit: `[feat] [v0.1.0] B端预约查询与宠物历史`

## Task 13: B 端核销

**Files:** Create `server/internal/logic/shop/verify.go`

**Step 1:** POST `/api/shop/app/verify`（前台/店长）：三重校验（本店+角色→50001、status=2→50003、无 status=3→50004）→ CAS 2→5 → oper_log VERIFY。
**Step 2:** 测试：B-03/B-04/B-05/B-06；与取消并发仅一方成功（B-14/C-09）。
**Step 3:** Commit: `[feat] [v0.1.0] B端到店核销与并发互斥`

## Task 14: B 端服务进度与未到店

**Files:** Create `server/internal/logic/shop/service.go`、`server/internal/logic/shop/noshow.go`

**Step 1:** POST `/api/shop/app/service-start|service-finish`（服务/店长）：CAS 5→6、6→7，跳级 50009；oper_log START/FINISH；重复完成返回"已完成登记"。
**Step 2:** POST `/api/shop/app/noshow`（店长）：status=2 + now>slot.end_time（否则 50005）+ 未核销 + 无 status=3；原因必填（50006，DB 非空）→ CAS 2→8；oper_log NOSHOW。
**Step 3:** 测试：B-07/B-08/B-09。
**Step 4:** Commit: `[feat] [v0.1.0] B端服务进度登记与未到店标记`

## Task 15: B 端取消审批与退款跟进

**Files:** Create `server/internal/logic/shop/audit.go`

**Step 1:** GET `/api/shop/cancel/list`（客服/店长）：仅 status=3，申请时间正序，分页。
**Step 2:** POST `/api/shop/cancel/audit`：同意→CAS 3→4 + refund_type=1 退款单 + 释放名额；驳回→CAS 3→2；一次性（50007）；oper_log AUDIT。
**Step 3:** GET `/api/shop/refund/list` + POST `/api/shop/refund/retry`（仅客服，店长 50008）：SETNX 60s；只改退款单，记 retry_count/fail_msg；oper_log RETRY。
**Step 4:** 测试：B-10/B-11/B-12。
**Step 5:** Commit: `[feat] [v0.1.0] B端取消审批与退款重试`

## Task 16: B 端支付异常单

**Files:** Create `server/internal/logic/shop/pexception.go`

**Step 1:** GET `/api/shop/pay-exception/list`（店长/客服，默认待处理）；登记逻辑在 Task 8 回调中完成，此处补人工重试退款入口（客服）；重复登记 50010。
**Step 2:** 测试：B-13。
**Step 3:** Commit: `[feat] [v0.1.0] B端支付异常单管理`

## Task 17: B 端日清台账与导出

**Files:** Create `server/internal/logic/shop/ledger.go`

**Step 1:** GET `/api/shop/ledger/daily?date=`：单一聚合方法按 slot 分组输出五类计数 + 四项金额（SQL 口径按 B端文档 §12.5），payExceptionPending 汇总。
**Step 2:** GET `/api/shop/ledger/export`：CSV 导出预约台账/退款流水/未到店清单/异常单/日清汇总（明细行带 app_id，汇总置顶与页面一致）；写 oper_log EXPORT（店长）。
**Step 3:** 测试：B-15（四笔记录口径示例）与 B-16（下钻一致）。
**Step 4:** Commit: `[feat] [v0.1.0] B端日清台账与导出`

## Task 18: C 端前端页面（按原型图）

**Files:** Create `server/public/c/`：`home.html`、`booking.html`、`lock.html`、`bookings.html`、`booking-detail.html`、`refund.html` + `c/common.js`

**Step 1:** 以 `原型图/` 对应文件为视觉基准（保留 Tailwind 结构与配色），替换 modao CDN 为 `https://cdn.tailwindcss.com`，移除写死数据。
**Step 2:** common.js：封装 fetch（带 X-User-Id，默认 user_1001，提供切换用户调试）、错误码→文案映射、倒计时组件。
**Step 3:** 页面接线：home（GET slot/list，名额已满置灰）、booking（选宠物+预约须知勾选→POST book）、lock（5 分钟倒计时→支付→轮询详情）、bookings（状态分组）、booking-detail（状态文案+按钮显隐+精简日志+同步状态）、refund（退款状态+失败原因）。
**Step 4:** 手工走查 C-01 正常链路。
**Step 5:** Commit: `[feat] [v0.1.0] C端前端页面（按原型图实现）`

## Task 19: B 端前端页面（按原型图）

**Files:** Create `server/public/b/`：`workbench.html`、`slots.html`、`bookings-admin.html`、`frontdesk.html`、`service.html`、`cancel-review.html`、`refund-tracking.html`、`payment-issue.html`、`no-show.html`、`ledger.html` + `b/common.js`

**Step 1:** 同 Task 18 基准；common.js 带 X-Staff-Id 角色切换（manager_01/front_01/svc_01/cs_01），按角色控制菜单与按钮显隐。
**Step 2:** 各页面接线：时段创建/暂停（50002 提示）、预约列表检索分页、核销（三重校验提示）、服务登记、取消审批（同意/驳回）、退款列表+重试、异常单、未到店（原因必填弹窗）、台账（四金额+五计数+下钻+导出）。
**Step 3:** 手工走查完整场景：正常接待 / 名额循环 / 取消三分支 / 收店核查。
**Step 4:** Commit: `[feat] [v0.1.0] B端前端页面（按原型图实现）`

## Task 20: 单元测试补齐与覆盖率

**Files:** Create/Modify `server/internal/**/*_test.go`

**Step 1:** 补齐约束文件要求的核心测试：状态机流转矩阵、名额 4 释放场景与冻结、并发抢位/取消核销互斥、支付幂等（同号/异号/关闭后）、台账口径（已收款≠已完成收入）、角色越权。
**Step 2:** `go test ./... -cover` 覆盖率输出留存到 `docs/evidence/coverage.txt`。
**Step 3:** Commit: `[test] [v0.1.0] 核心业务单元测试与覆盖率报告`

## Task 21: 端到端演示与验收自检

**Step 1:** 启动服务，按题目"必须演示的完整工作场景"五条主线逐条走查（正常接待、名额循环、取消分支、门店现场冲突、收店核查），记录证据截图/请求日志到 `docs/evidence/`。
**Step 2:** 对照 C 端 22 条 + B 端 16 条验收用例自检。
**Step 3:** Commit: `[docs] [v0.1.0] 演示记录与验收自检证据`

---

## 提交说明

- 提交格式遵循 `commit-convention.md`：`[类型] [版本号] [编号] 中文描述`
- feat/fix 需要"需求/Bug 编号"（飞书工作项）。**当前未提供真实需求编号**：涉及编号的提交将在执行时询问用户；无编号可提供时暂以 `[docs]`/`[test]` 类型提交或在用户授权下使用占位编号并记录假设。
- 所有提交与推送均在 `dev/02` 分支；禁止直接提交 main。
