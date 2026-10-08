# 存储与备份

[English](storage.md) · [文档目录](README.zh-CN.md)

## 数据目录

Core 将状态保存在 `MAILWAKE_DATA_DIR`，整个目录需要一起保留：

- `mailwake.db`：SQLite 配置、管理员访问凭据、文件夹进度和通知队列；SQLite 还可能生成 WAL 与共享内存文件。
- `secret.key`：用于加密已保存凭据和敏感配置的密钥。
- `core.lock`：保证一个数据目录同时由一个 Core 实例使用的进程锁。

凭据加密保护数据库内的敏感配置。包含密钥的完整目录及其备份需要设置私有文件权限。

## 数据库基线

当前开发版本在单个事务中执行 `internal/storage/migrations/001_baseline.sql`，创建新数据库并写入 `PRAGMA user_version=1`。已有版本 1 数据库直接打开，其他版本返回 `database_version_unsupported`。

首次发布前，数据库结构变更直接更新基线。结构变化后，开发测试使用新的临时数据目录。

## 备份与恢复

停止 Core，备份完整数据目录，再启动服务。恢复时先停止 Core，从同一份备份还原数据库与 `secret.key`。Docker 备份命令、升级和管理员恢复操作见[部署指南](deployment.zh-CN.md)。
