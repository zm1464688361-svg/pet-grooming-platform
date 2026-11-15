package consts

// 通用错误码（非业务校验类，兜底）
const (
	ErrInternal = 500 // 系统繁忙，请稍后重试
)

// C 端错误码（agentrules §18，禁止自创）
const (
	ErrNoPermission     = 40001 // 宠物不属于当前用户 / 无权操作该预约
	ErrSlotUnavailable  = 40002 // 时段不可预约或已开始
	ErrQuotaFull        = 40003 // 名额已满
	ErrPetDuplicated    = 40004 // 同宠物在本时段已有有效预约
	ErrOrderTimeout     = 40005 // 订单已超时关闭
	ErrCancelNotAllowed = 40006 // 当前状态不支持取消
	ErrIdempotentHit    = 40007 // 重复提交（幂等命中）
	ErrPayProcessing    = 40008 // 支付请求处理中，存在并发支付请求
	ErrPayNotAllowed    = 40009 // 当前状态不支持发起支付
	ErrCancelReasonNeed = 40010 // 取消原因必填
	ErrCancelDuplicated = 40011 // 重复提交取消申请
)

// B 端错误码（agentrules-B端 §15，禁止自创）
const (
	ErrShopForbidden    = 50001 // 无权操作其他门店数据
	ErrSlotHasBooking   = 50002 // 时段存在有效预约，不可修改/暂停/删除
	ErrStatusConflict   = 50003 // 当前状态不可执行该动作
	ErrAuditBlocking    = 50004 // 存在待处理取消申请，暂不可核销
	ErrSlotNotEnded     = 50005 // 时段未结束，不可标记未到店
	ErrNoShowReasonNeed = 50006 // 未到店原因必填
	ErrAuditDone        = 50007 // 取消申请已处理，不可重复审批
	ErrRefundForbidden  = 50008 // 无退款操作权限
	ErrSkipProgress     = 50009 // 服务进度不可跳级，请先登记开始
	ErrExcHandled       = 50010 // 支付异常单已处理，不可重复登记
)

// ErrorMessages 错误码 → 文案（固定，禁止修改）
var ErrorMessages = map[int]string{
	ErrInternal:         "系统繁忙，请稍后重试",
	ErrNoPermission:     "宠物不属于当前用户 / 无权操作该预约",
	ErrSlotUnavailable:  "时段不可预约或已开始",
	ErrQuotaFull:        "名额已满",
	ErrPetDuplicated:    "同宠物在本时段已有有效预约",
	ErrOrderTimeout:     "订单已超时关闭",
	ErrCancelNotAllowed: "当前状态不支持取消",
	ErrIdempotentHit:    "重复提交（幂等命中）",
	ErrPayProcessing:    "支付请求处理中，请勿重复点击",
	ErrPayNotAllowed:    "当前状态不支持发起支付",
	ErrCancelReasonNeed: "请填写取消原因",
	ErrCancelDuplicated: "已提交取消申请，请勿重复提交",

	ErrShopForbidden:    "无权操作其他门店数据",
	ErrSlotHasBooking:   "时段存在有效预约，不可修改/暂停/删除",
	ErrStatusConflict:   "当前状态不可执行该动作",
	ErrAuditBlocking:    "存在待处理取消申请，暂不可核销",
	ErrSlotNotEnded:     "时段未结束，不可标记未到店",
	ErrNoShowReasonNeed: "未到店原因必填",
	ErrAuditDone:        "取消申请已处理，不可重复审批",
	ErrRefundForbidden:  "无退款操作权限",
	ErrSkipProgress:     "服务进度不可跳级，请先登记开始",
	ErrExcHandled:       "支付异常单已处理，不可重复登记",
}

// Messages C 端固定返回文案（agentrules §17，禁止修改）
const (
	MsgBookSuccess   = "预约成功，名额保留5分钟，请尽快完成支付"
	MsgQuotaFull     = "该时段名额已抢光，请选择其他时段"
	MsgPetDuplicated = "该宠物已预约本时段，请勿重复预约"
	MsgSlotStarted   = "该时段已开始，无法预约"
	MsgTimeoutCancel = "预约超时未支付，订单已自动取消，名额已释放"
	MsgPayProcessing = "支付请求处理中，请勿重复点击"
	MsgPaySuccess    = "支付成功，预约已确认，请按时到店"
	MsgCancelAudit   = "已提交取消申请，等待客服审核，审核期间门店暂无法核销"
	MsgRefundFailed  = "退款失败，请联系客服跟进，预约状态不受影响"
	MsgNoShow        = "门店已标记本次未到店，本次预约不自动退款"
)
