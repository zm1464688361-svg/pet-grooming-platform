package consts

// 时段状态（B 端四态口径，见 docs/plans/assumptions.md 假设 2）
const (
	SlotActive  = 1 // 待生效（可预约）
	SlotPaused  = 2 // 暂停
	SlotOngoing = 3 // 进行中
	SlotEnded   = 4 // 已结束
)

// 员工角色
const (
	RoleManager = 1 // 店长
	RoleFront   = 2 // 前台
	RoleService = 3 // 服务人员
	RoleSupport = 4 // 客服
)

// 支付单状态
const (
	PayStatusUnpaid = 1
	PayStatusPaid   = 2
	PayStatusFailed = 3
)

// 退款类型 / 退款状态（type 3 为登记假设，见 docs/plans/assumptions.md 假设 1）
const (
	RefundTypeCancel    = 1 // 取消退款
	RefundTypeDuplicate = 2 // 重复支付退款
	RefundTypeException = 3 // 支付异常退款

	RefundStatusPending = 1 // 待退款
	RefundStatusSuccess = 2 // 退款成功
	RefundStatusFailed  = 3 // 退款失败
)

// 审批结论
const (
	AuditApprove = 1 // 同意
	AuditReject  = 2 // 驳回
)

// 支付异常类型 / 状态
const (
	ExcTypeClosedAfterPay = 1 // 关闭后支付
	ExcTypeDuplicate      = 2 // 重复扣款

	ExcStatusPending    = 1 // 待处理
	ExcStatusRefunded   = 2 // 已退款
	ExcStatusRefundFail = 3 // 退款失败
)

// 宠物状态 / 员工状态
const (
	PetStatusActive   = 1
	PetStatusDisabled = 2

	StaffStatusActive   = 1
	StaffStatusDisabled = 2
)
