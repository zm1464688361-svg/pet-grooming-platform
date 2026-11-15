-- 宠物洗护预约平台（题目 02）数据库 Schema
-- 状态口径与 .agent 约束一致，禁止修改业务口径。

CREATE TABLE IF NOT EXISTS `shop` (
  `shop_id`   VARCHAR(64) NOT NULL,
  `shop_name` VARCHAR(128) NOT NULL,
  `address`   VARCHAR(255) NULL,
  `created_at` DATETIME NOT NULL,
  PRIMARY KEY (`shop_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `service_item` (
  `item_id`    VARCHAR(64) NOT NULL,
  `shop_id`    VARCHAR(64) NOT NULL,
  `item_name`  VARCHAR(128) NOT NULL,
  `price_cent` BIGINT NOT NULL,               -- 单位：分，固定 9900
  `status`     TINYINT NOT NULL DEFAULT 1,    -- 1上架 2下架
  PRIMARY KEY (`item_id`),
  KEY `idx_shop` (`shop_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `user` (
  `user_id`    VARCHAR(64) NOT NULL,
  `user_name`  VARCHAR(64) NOT NULL,
  `phone`      VARCHAR(20) NOT NULL,
  `created_at` DATETIME NOT NULL,
  PRIMARY KEY (`user_id`),
  KEY `idx_phone` (`phone`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS `pet` (
  `pet_id`     VARCHAR(64) NOT NULL,
  `user_id`    VARCHAR(64) NOT NULL,
  `pet_name`   VARCHAR(64) NOT NULL,
  `pet_type`   VARCHAR(20) NULL,              -- 猫/狗等，展示用
  `status`     TINYINT NOT NULL DEFAULT 1,    -- 1启用 2停用
  `created_at` DATETIME NOT NULL,
  PRIMARY KEY (`pet_id`),
  KEY `idx_user` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 时段：1可预约(待生效) 2暂停 3进行中 4已结束
CREATE TABLE IF NOT EXISTS `time_slot` (
  `slot_id`      VARCHAR(64) NOT NULL,
  `shop_id`      VARCHAR(64) NOT NULL,
  `start_time`   DATETIME NOT NULL,
  `end_time`     DATETIME NOT NULL,
  `max_capacity` INT NOT NULL DEFAULT 3,        -- 固定 3，忽略前端传值
  `status`       TINYINT NOT NULL DEFAULT 1,
  `created_by`   VARCHAR(64) NOT NULL,          -- 店长 staff_id
  `created_at`   DATETIME NOT NULL,
  PRIMARY KEY (`slot_id`),
  KEY `idx_shop_start` (`shop_id`, `start_time`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 预约：状态 1待支付 2已确认 3待客服审核取消 4取消成功 5已核销 6服务中 7服务完成 8未到店 9待支付超时关闭
CREATE TABLE IF NOT EXISTS `appointment` (
  `app_id`           VARCHAR(64) NOT NULL,
  `user_id`          VARCHAR(64) NOT NULL,
  `pet_id`           VARCHAR(64) NOT NULL,
  `slot_id`          VARCHAR(64) NOT NULL,
  `shop_id`          VARCHAR(64) NOT NULL,
  `item_id`          VARCHAR(64) NOT NULL,
  `price`            BIGINT NOT NULL,           -- 单位：分，固定 9900
  `status`           TINYINT NOT NULL DEFAULT 1,
  `user_remark`      VARCHAR(255) NULL,         -- C 端备注（双向可见）
  `shop_remark`      VARCHAR(255) NULL,         -- 门店内部备注（C 端不可见）
  `lock_expire_at`   DATETIME NULL,             -- 待支付锁定到期：now+5min
  `cancel_apply_at`  DATETIME NULL,             -- 取消申请时间
  `cancel_reason`    VARCHAR(255) NULL,         -- 取消原因（<2h 必填）
  `audit_result`     TINYINT NULL,              -- 1同意 2驳回（客服审批结论）
  `audit_at`         DATETIME NULL,
  `audit_staff_id`   VARCHAR(64) NULL,
  `verify_at`        DATETIME NULL,             -- 核销时间
  `verify_staff_id`  VARCHAR(64) NULL,
  `service_start_at` DATETIME NULL,
  `service_finish_at` DATETIME NULL,
  `noshow_at`        DATETIME NULL,
  `noshow_reason`    VARCHAR(255) NULL,         -- 非空约束：未到店原因必填
  `created_at`       DATETIME NOT NULL,
  `updated_at`       DATETIME NOT NULL,
  PRIMARY KEY (`app_id`),
  UNIQUE KEY `uk_pet_slot` (`pet_id`, `slot_id`),   -- 同宠物同时段仅一笔有效预约
  KEY `idx_slot_status` (`slot_id`, `status`),
  KEY `idx_user` (`user_id`),
  KEY `idx_shop_created` (`shop_id`, `created_at`),
  CONSTRAINT `ck_noshow_reason` CHECK (`noshow_reason` IS NULL OR CHAR_LENGTH(`noshow_reason`) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 支付单：一单一笔成功支付（按预约维度唯一）
CREATE TABLE IF NOT EXISTS `payment` (
  `payment_id`     VARCHAR(64) NOT NULL,
  `app_id`         VARCHAR(64) NOT NULL,
  `amount`         BIGINT NOT NULL,             -- 单位：分
  `status`         TINYINT NOT NULL,            -- 1待支付 2成功 3失败
  `transaction_id` VARCHAR(128) NULL,           -- 成功支付的交易号
  `paid_at`        DATETIME NULL,
  `created_at`     DATETIME NOT NULL,
  PRIMARY KEY (`payment_id`),
  UNIQUE KEY `uk_app` (`app_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 支付流水日志：交易号唯一，一笔真实资金只落一条（对账与异常识别依据）
CREATE TABLE IF NOT EXISTS `payment_log` (
  `log_id`         VARCHAR(64) NOT NULL,
  `app_id`         VARCHAR(64) NOT NULL,
  `transaction_id` VARCHAR(128) NOT NULL,
  `amount`         BIGINT NOT NULL,
  `status`         TINYINT NOT NULL,            -- 1已确认 2已拒绝
  `created_at`     DATETIME NOT NULL,
  PRIMARY KEY (`log_id`),
  UNIQUE KEY `uk_txn` (`transaction_id`),
  KEY `idx_app` (`app_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 退款单：refund_type 1取消退款 2重复支付退款 3支付异常退款；status 1待退款 2退款成功 3退款失败
CREATE TABLE IF NOT EXISTS `refund` (
  `refund_id`   VARCHAR(64) NOT NULL,
  `app_id`      VARCHAR(64) NOT NULL,
  `refund_type` TINYINT NOT NULL,
  `amount`      BIGINT NOT NULL,
  `status`      TINYINT NOT NULL DEFAULT 1,
  `fail_msg`    VARCHAR(255) NULL,              -- 渠道失败原因
  `retry_count` INT NOT NULL DEFAULT 0,
  `refunded_at` DATETIME NULL,
  `created_at`  DATETIME NOT NULL,
  PRIMARY KEY (`refund_id`),
  KEY `idx_app` (`app_id`),
  KEY `idx_status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- B 端员工：role 1店长 2前台 3服务人员 4客服
CREATE TABLE IF NOT EXISTS `shop_staff` (
  `staff_id`   VARCHAR(64) PRIMARY KEY,
  `shop_id`    VARCHAR(64) NOT NULL,
  `staff_name` VARCHAR(64) NOT NULL,
  `role`       TINYINT NOT NULL,
  `status`     TINYINT NOT NULL DEFAULT 1,     -- 1在职 2停用
  `created_at` DATETIME NOT NULL,
  KEY `idx_shop_role` (`shop_id`, `role`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 操作日志：只追加不修改不删除
CREATE TABLE IF NOT EXISTS `oper_log` (
  `log_id`     VARCHAR(64) PRIMARY KEY,
  `shop_id`    VARCHAR(64) NOT NULL,
  `staff_id`   VARCHAR(64) NOT NULL,
  `app_id`     VARCHAR(64) NULL,
  `action`     VARCHAR(32) NOT NULL,           -- VERIFY/START/FINISH/NOSHOW/AUDIT/RETRY/EXPORT/SLOT_CREATE/...
  `from_status` TINYINT NULL,
  `to_status`  TINYINT NULL,
  `remark`     VARCHAR(255) NULL,
  `created_at` DATETIME NOT NULL,
  KEY `idx_app` (`app_id`),
  KEY `idx_shop_time` (`shop_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 支付异常单：订单已关闭/已取消后又收到支付成功
CREATE TABLE IF NOT EXISTS `pay_exception` (
  `exc_id`         VARCHAR(64) PRIMARY KEY,
  `shop_id`        VARCHAR(64) NOT NULL,
  `app_id`         VARCHAR(64) NOT NULL,
  `transaction_id` VARCHAR(128) NOT NULL,
  `amount`         BIGINT NOT NULL,
  `exc_type`       TINYINT NOT NULL,           -- 1关闭后支付 2重复扣款
  `refund_id`      VARCHAR(64) NULL,
  `status`         TINYINT NOT NULL,           -- 1待处理 2已退款 3退款失败
  `created_at`     DATETIME NOT NULL,
  UNIQUE KEY `uk_txn` (`transaction_id`),
  KEY `idx_shop_status` (`shop_id`, `status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
