// Package consts 业务常量与状态枚举。
// 口径与 .agent 约束文件一致，禁止新增状态、禁止私自跳转。
package consts

// 预约状态枚举（唯一口径）
const (
	StatusPending       = 1 // 待支付（占用名额，5 分钟倒计时）
	StatusConfirmed     = 2 // 已确认（支付成功，名额继续占用）
	StatusCancelAudit   = 3 // 待客服审核取消（名额冻结，不可核销）
	StatusCancelled     = 4 // 取消成功（名额已释放，可关联退款单）
	StatusVerified      = 5 // 已核销
	StatusInService     = 6 // 服务中
	StatusFinished      = 7 // 服务完成
	StatusNoShow        = 8 // 门店标记未到店
	StatusTimeoutClosed = 9 // 待支付超时关闭（终态）
)

// 有效预约状态（参与名额占用与唯一性校验）
var ValidStatuses = []int{StatusPending, StatusConfirmed, StatusCancelAudit}

// 占用名额状态（红线 4.1.4：已核销(5)/服务中(6)/服务完成(7)/未到店(8) 名额永久占用）。
// 建单容量与同宠物唯一校验必须统计此全集，防止核销后重复预约绕过容量。
var OccupiedStatuses = []int{StatusPending, StatusConfirmed, StatusCancelAudit,
	StatusVerified, StatusInService, StatusFinished, StatusNoShow}

// 业务固定常量（.agent 约束口径，禁止修改）
const (
	ShopId            = "shop_001" // 单门店
	ItemId            = "item_wash_001"
	PriceCent         = 9900 // 预约款固定 9900 分
	MaxCapacity       = 3    // 单时段容量固定 3
	LockMinutes       = 5    // 待支付锁定 5 分钟
	CancelWindowHours = 2    // 取消窗口 2 小时（秒级比较，恰好 2h 按 ≥2h）
	PayLockSeconds    = 30   // 支付幂等锁 30 秒
)

// IsValidStatus 判断状态是否为有效预约（占用名额）。
func IsValidStatus(status int) bool {
	return status == StatusPending || status == StatusConfirmed || status == StatusCancelAudit
}

// AppointmentStatusNames 状态文案映射（C/B 端共用）。
var AppointmentStatusNames = map[int]string{
	StatusPending:       "待支付",
	StatusConfirmed:     "已确认",
	StatusCancelAudit:   "待客服审核取消",
	StatusCancelled:     "取消成功",
	StatusVerified:      "已核销",
	StatusInService:     "服务中",
	StatusFinished:      "服务完成",
	StatusNoShow:        "未到店",
	StatusTimeoutClosed: "待支付超时关闭",
}

// IsFinalStatus 终态判断：4/5/6/7/8/9 不可回退。
func IsFinalStatus(status int) bool {
	return status >= StatusCancelled
}
