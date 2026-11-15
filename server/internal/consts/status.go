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
