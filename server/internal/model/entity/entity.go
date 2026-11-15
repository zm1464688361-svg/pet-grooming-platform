// Package entity 数据库表实体（与 manifest/sql/schema.sql 对应）。
package entity

import "time"

type Shop struct {
	ShopId    string    `json:"shopId" orm:"shop_id"`
	ShopName  string    `json:"shopName" orm:"shop_name"`
	Address   string    `json:"address" orm:"address"`
	CreatedAt time.Time `json:"createdAt" orm:"created_at"`
}

type ServiceItem struct {
	ItemId    string `json:"itemId" orm:"item_id"`
	ShopId    string `json:"shopId" orm:"shop_id"`
	ItemName  string `json:"itemName" orm:"item_name"`
	PriceCent int64  `json:"priceCent" orm:"price_cent"`
	Status    int    `json:"status" orm:"status"`
}

type User struct {
	UserId    string    `json:"userId" orm:"user_id"`
	UserName  string    `json:"userName" orm:"user_name"`
	Phone     string    `json:"phone" orm:"phone"`
	CreatedAt time.Time `json:"createdAt" orm:"created_at"`
}

type Pet struct {
	PetId     string    `json:"petId" orm:"pet_id"`
	UserId    string    `json:"userId" orm:"user_id"`
	PetName   string    `json:"petName" orm:"pet_name"`
	PetType   string    `json:"petType" orm:"pet_type"`
	Status    int       `json:"status" orm:"status"`
	CreatedAt time.Time `json:"createdAt" orm:"created_at"`
}

type TimeSlot struct {
	SlotId      string    `json:"slotId" orm:"slot_id"`
	ShopId      string    `json:"shopId" orm:"shop_id"`
	StartTime   time.Time `json:"startTime" orm:"start_time"`
	EndTime     time.Time `json:"endTime" orm:"end_time"`
	MaxCapacity int       `json:"maxCapacity" orm:"max_capacity"`
	Status      int       `json:"status" orm:"status"`
	CreatedBy   string    `json:"createdBy" orm:"created_by"`
	CreatedAt   time.Time `json:"createdAt" orm:"created_at"`
}

type Appointment struct {
	AppId           string     `json:"appId" orm:"app_id"`
	UserId          string     `json:"userId" orm:"user_id"`
	PetId           string     `json:"petId" orm:"pet_id"`
	SlotId          string     `json:"slotId" orm:"slot_id"`
	ShopId          string     `json:"shopId" orm:"shop_id"`
	ItemId          string     `json:"itemId" orm:"item_id"`
	Price           int64      `json:"price" orm:"price"`
	Status          int        `json:"status" orm:"status"`
	UserRemark      string     `json:"userRemark" orm:"user_remark"`
	ShopRemark      string     `json:"shopRemark" orm:"shop_remark"`
	LockExpireAt    *time.Time `json:"lockExpireAt" orm:"lock_expire_at"`
	CancelApplyAt   *time.Time `json:"cancelApplyAt" orm:"cancel_apply_at"`
	CancelReason    string     `json:"cancelReason" orm:"cancel_reason"`
	AuditResult     *int       `json:"auditResult" orm:"audit_result"`
	AuditAt         *time.Time `json:"auditAt" orm:"audit_at"`
	AuditStaffId    string     `json:"auditStaffId" orm:"audit_staff_id"`
	VerifyAt        *time.Time `json:"verifyAt" orm:"verify_at"`
	VerifyStaffId   string     `json:"verifyStaffId" orm:"verify_staff_id"`
	ServiceStartAt  *time.Time `json:"serviceStartAt" orm:"service_start_at"`
	ServiceFinishAt *time.Time `json:"serviceFinishAt" orm:"service_finish_at"`
	NoShowAt        *time.Time `json:"noshowAt" orm:"noshow_at"`
	NoShowReason    string     `json:"noshowReason" orm:"noshow_reason"`
	CreatedAt       time.Time  `json:"createdAt" orm:"created_at"`
	UpdatedAt       time.Time  `json:"updatedAt" orm:"updated_at"`
}

type Payment struct {
	PaymentId     string     `json:"paymentId" orm:"payment_id"`
	AppId         string     `json:"appId" orm:"app_id"`
	Amount        int64      `json:"amount" orm:"amount"`
	Status        int        `json:"status" orm:"status"`
	TransactionId string     `json:"transactionId" orm:"transaction_id"`
	PaidAt        *time.Time `json:"paidAt" orm:"paid_at"`
	CreatedAt     time.Time  `json:"createdAt" orm:"created_at"`
}

type PaymentLog struct {
	LogId         string    `json:"logId" orm:"log_id"`
	AppId         string    `json:"appId" orm:"app_id"`
	TransactionId string    `json:"transactionId" orm:"transaction_id"`
	Amount        int64     `json:"amount" orm:"amount"`
	Status        int       `json:"status" orm:"status"`
	CreatedAt     time.Time `json:"createdAt" orm:"created_at"`
}

type Refund struct {
	RefundId   string     `json:"refundId" orm:"refund_id"`
	AppId      string     `json:"appId" orm:"app_id"`
	RefundType int        `json:"refundType" orm:"refund_type"`
	Amount     int64      `json:"amount" orm:"amount"`
	Status     int        `json:"status" orm:"status"`
	FailMsg    string     `json:"failMsg" orm:"fail_msg"`
	RetryCount int        `json:"retryCount" orm:"retry_count"`
	RefundedAt *time.Time `json:"refundedAt" orm:"refunded_at"`
	CreatedAt  time.Time  `json:"createdAt" orm:"created_at"`
}

type ShopStaff struct {
	StaffId   string    `json:"staffId" orm:"staff_id"`
	ShopId    string    `json:"shopId" orm:"shop_id"`
	StaffName string    `json:"staffName" orm:"staff_name"`
	Role      int       `json:"role" orm:"role"`
	Status    int       `json:"status" orm:"status"`
	CreatedAt time.Time `json:"createdAt" orm:"created_at"`
}

type OperLog struct {
	LogId      string    `json:"logId" orm:"log_id"`
	ShopId     string    `json:"shopId" orm:"shop_id"`
	StaffId    string    `json:"staffId" orm:"staff_id"`
	AppId      string    `json:"appId" orm:"app_id"`
	Action     string    `json:"action" orm:"action"`
	FromStatus *int      `json:"fromStatus" orm:"from_status"`
	ToStatus   *int      `json:"toStatus" orm:"to_status"`
	Remark     string    `json:"remark" orm:"remark"`
	CreatedAt  time.Time `json:"createdAt" orm:"created_at"`
}

type PayException struct {
	ExcId         string     `json:"excId" orm:"exc_id"`
	ShopId        string     `json:"shopId" orm:"shop_id"`
	AppId         string     `json:"appId" orm:"app_id"`
	TransactionId string     `json:"transactionId" orm:"transaction_id"`
	Amount        int64      `json:"amount" orm:"amount"`
	ExcType       int        `json:"excType" orm:"exc_type"`
	RefundId      string     `json:"refundId" orm:"refund_id"`
	Status        int        `json:"status" orm:"status"`
	CreatedAt     time.Time  `json:"createdAt" orm:"created_at"`
}
