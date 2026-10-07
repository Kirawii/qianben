# 0.1 实现自审与架构落地说明

截至 2026-10-06。产品口径沿用 PRD v0.3.2；本文件说明相对 TDD v0.2 的已落地实现和明确边界。

## 已落地结构

Go 模块化单体由 `domain`（金额、接受的事实）、`accounting`（确定性会计模板）、`evidence`（保守通知解析）、`store`（事务命令和数据库适配）、`httpapi` 及单个 CLI 入口组成。API 和 worker 共用 `writeEvent`，所有正式分录都经过受限的 `qb.post` 函数。Android 使用 Kotlin 原生视图、Room、NotificationListenerService、WorkManager；没有引入模型、OCR 或第三方分析 SDK。

PostgreSQL 17 是唯一账务权威。app 角色没有直接 Journal/Entry/PostingRevision 写权限；独立 NOLOGIN owner 持有入账及删除函数。实际账户、外部账户与支付工具分开，微信/支付宝支付通道不会被当作银行卡余额。来源 episode 与 immutable snapshot 及实际事件身份分开。

## 落地时的简化

- 当前使用参数化 pgx 查询，尚未引入 sqlc 生成层；`store` 是统一事务边界，禁止处理器直接写分录。后续数据访问增多再采用 sqlc，不增加当前可运行路径的生成依赖。
- 报表在一个 repeatable-read 事务中读取分录、现实时间和账本版本，按请求同步重建三套视图。尚未启用预计算 `projections`。这样无需处理异步缓存落后，也不会三套视图各读不同版本。
- worker 每次将一个纯数据库证据任务完整放在短事务中，先锁 ledger，再锁 outbox。没有外部副作用和跨事务租约，崩溃回滚后可重试。TDD 的 jobs/lease/fencing 为以后外部模型或较长任务预留，不声称目前已实现这些任务机制。
- 自动解析通知不会猜经济发生时间或实际付款账户；第一版可靠主路径是手动确认和固定格式 CSV。跨来源同一事件由用户确认合并，弱模板和同金额时间窗口不自动决定身份。
- 复杂经济关系不会自动迁移分摊：已退款的原消费财务字段、已关联转账及带经济关系的合并拆分受到保护。提供退款修订、转账解除后再修复，避免产生看似成功的错误关系。

这些简化改变工程实现方式，不改变退款月份、待查款先更新余额、固定资产原值等已确认口径。v0.2 中更长远的租约、投影、正式对账等描述应作为后续设计。

## 自审发现并修复

| 问题 | 修复及验证 |
|---|---|
| Journal 与 Entry 共用触发器，CASE 访问不存在的 record 字段 | 使用独立 IF 分支；真实 PostgreSQL 测试检出后修复 |
| ledger 级联删除时交叉 FK 在中途检查 | FK 延迟到提交，删除由授权函数原子完成；完整删除测试通过 |
| 将每条暂记分录取绝对值会累计冲销 | 先求暂记账户净余额，再汇总待查余额 |
| 冲销按新事件类型归类会错分旧现金流 | 通过原 Journal 引用找到旧 PostingRevision 的类型 |
| JSONB 回读的字段顺序不同 | 幂等测试比较语义 JSON，不比较序列化字节顺序 |
| delivery 重投 envelope 与 snapshot 内容混为同一哈希 | 分开 delivery hash 与内容 hash；A→B→A 保留不同 snapshot |
| 合并关系被误当作必须先解除的经济关系 | 身份关系与退款/转账关系分开保护；合并后再拆分测试通过 |
| 转账转出端及原消费时间/币种修改会损坏关系 | 两端保护，并覆盖时间、币种、账户、类型及金额 |
| 只用内存 UUID 的客户端重试可能重复创建消费 | 写前持久化完整命令和固定 command_id，确认后移除 |
| 删除一个账本清空整个客户端队列 | 按 ledger 清理；Android 仪器测试验证另一个 ledger 保留 |
| 切换服务地址后把旧通知上传给新服务 | 本地队列绑定原 endpoint，不匹配时保留拒绝状态 |
| Windows 中文目录的 Android 构建限制 | 显式路径检查兼容设置，并实际构建验证 |
| minSdk 26 使用 API 33 才有的 readNBytes | 改为有上限的传统流读取；Lint 检出后修复 |
| 纳秒与 PostgreSQL 微秒导致创建内容比较不一致 | 创建和用户确认时间统一到微秒精度 |
| 宽泛财务词可能从微信聊天捕获内容 | 增加财务服务标题限制，银行与渠道覆盖仍需真机验证 |

## 保留的限制

当前 API 认证是管理员发放随机令牌，适用于个人开发试用；没有面向公众的注册、找回、多人协作或 OAuth 服务。数据隔离依赖服务端账本授权及受限数据库角色，尚未采用数据库 RLS。公网部署、压力测试、安全审计、真实银行通知覆盖、耗电和灾备删除策略均不是本次本机验收的结论。

既存复杂退款分摊、报销批次对账、折旧/处置/估值及多币种后移。发生日期只有 DAY 且跨过启用时点时保持待确认；不以借贷平衡声称已经与银行对账。

Android SDK 行为参考：[NotificationListenerService](https://developer.android.com/reference/android/service/notification/NotificationListenerService)、[WorkManager 工作管理](https://developer.android.com/develop/background-work/background-tasks/persistent/how-to/manage-work)、[AGP 8.9 兼容要求](https://developer.android.com/build/releases/agp-8-9-0-release-notes)。同行通知入口调研来源及不能照搬的去重方式，见仓库原架构审查文档。
