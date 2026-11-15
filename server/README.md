# pet-grooming server

宠物洗护预约平台后端（题目 02）。

## 环境要求

- Go 1.22+
- MySQL 8.0（本机 3306，库名 `pet_grooming`，见 `manifest/config/config.yaml`）
- Redis 7（本机 6379，推荐 `docker compose up -d redis`）

## 快速开始

```powershell
# 1. 准备数据库（本机 MySQL，root 密码见 config.yaml）
mysql --host=127.0.0.1 --user=root --password=xxx < manifest/sql/schema.sql
mysql --host=127.0.0.1 --user=root --password=xxx < manifest/sql/seed.sql

# 2. 启动 Redis
docker compose up -d redis

# 3. 启动服务
go run main.go
# 健康检查: http://127.0.0.1:8000/health
```

## 业务常量（禁止修改口径）

预约款 9900 分｜单时段容量 3｜未付款锁定 5 分钟｜取消窗口 2 小时（秒级，恰好 2h 按 ≥2h）｜支付幂等锁 30 秒。

状态机与名额规则见仓库根目录 `AGENTS.md` 与 `.agent/` 约束文件。
