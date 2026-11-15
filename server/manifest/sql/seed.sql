-- 预置数据（业务基线：1 家门店 / 1 个服务项目 / 3 个未来时段）
-- 注意：时段基于 NOW() 生成，重复执行前请先 TRUNCATE（本脚本不幂等，仅初始化用）。

INSERT INTO `shop` (`shop_id`, `shop_name`, `address`, `created_at`) VALUES
('shop_001', '毛球家宠物洗护·望京店', '朝阳区望京SOHO T1 一层', NOW());

INSERT INTO `service_item` (`item_id`, `shop_id`, `item_name`, `price_cent`, `status`) VALUES
('item_wash_001', 'shop_001', '宠物洗护·基础全套', 9900, 1);

-- C 端用户（3 人）
INSERT INTO `user` (`user_id`, `user_name`, `phone`, `created_at`) VALUES
('user_1001', '张小明', '13800001001', NOW()),
('user_1002', '李小红', '13800001002', NOW()),
('user_1003', '王小军', '13800001003', NOW());

-- 宠物（含 1 只停用，用于验证停用宠物不可预约）
INSERT INTO `pet` (`pet_id`, `user_id`, `pet_name`, `pet_type`, `status`, `created_at`) VALUES
('pet_2001', 'user_1001', '豆豆', '狗', 1, NOW()),
('pet_2002', 'user_1001', '咪咪', '猫', 1, NOW()),
('pet_2003', 'user_1002', '旺财', '狗', 1, NOW()),
('pet_2004', 'user_1002', '雪球', '猫', 2, NOW()),
('pet_2005', 'user_1003', '球球', '狗', 1, NOW());

-- B 端员工（4 角色，客服被授权 shop_001）
INSERT INTO `shop_staff` (`staff_id`, `shop_id`, `staff_name`, `role`, `status`, `created_at`) VALUES
('manager_01', 'shop_001', '王店长', 1, 1, NOW()),
('front_01',   'shop_001', '刘前台', 2, 1, NOW()),
('svc_01',     'shop_001', '陈师傅', 3, 1, NOW()),
('cs_01',      'shop_001', '赵客服', 4, 1, NOW());

-- 3 个未来时段：今天 +3h、明天 10:00、后天 14:00（容量固定 3）
INSERT INTO `time_slot` (`slot_id`, `shop_id`, `start_time`, `end_time`, `max_capacity`, `status`, `created_by`, `created_at`) VALUES
('slot_01', 'shop_001', DATE_ADD(NOW(), INTERVAL 3 HOUR), DATE_ADD(NOW(), INTERVAL 4 HOUR), 3, 1, 'manager_01', NOW()),
('slot_02', 'shop_001', CONCAT(CURDATE() + INTERVAL 1 DAY, ' 10:00:00'), CONCAT(CURDATE() + INTERVAL 1 DAY, ' 11:00:00'), 3, 1, 'manager_01', NOW()),
('slot_03', 'shop_001', CONCAT(CURDATE() + INTERVAL 2 DAY, ' 14:00:00'), CONCAT(CURDATE() + INTERVAL 2 DAY, ' 15:00:00'), 3, 1, 'manager_01', NOW());
