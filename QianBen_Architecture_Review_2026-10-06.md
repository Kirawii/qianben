# 钱本架构：同行调研与自审记录

2026-10-06 · 审查对象：TDD v0.1 · 修订结果：TDD v0.2 · 后端已确定 Go

结论：保留 Kotlin Android＋Go 模块化单体＋PostgreSQL。采集部分借鉴字段抽取、模板适配、权限诊断和本地持久化；正式账务继续使用钱本自己的证据→接受事实→确定性会计流程。v0.1 有多处实现边界缺口，已在 v0.2 修订；目前只有文档、没有应用源码、数据库原型或真机测试结果。

## 1. 证据范围

调研以项目自己的 README、固定提交源码和官方 API 文档为依据。开源项目支持列表及 commercial FAQ 属于作者说明，不等于独立覆盖率验证。未将作者给出的准确率作为钱本目标；未测试这些 APK。GitHub 源码使用固定 SHA，避免未来 main/master 变化改变审查对象。

| 参考 | 实际核验内容 | 借鉴与边界 |
|---|---|---|
| [AutoAccounting README](https://github.com/AutoAccountingOrg/AutoAccounting/blob/34b38f82ca9ad6d7905c715c66891977f8809c88/README.md) | 多入口模式，包括通知、Hook、短信及屏幕识别；具体模式有权限条件 | 适配器抽象有价值，不能据完整模式的支持列表推断普通通知模式覆盖 |
| [AutoAccounting NotificationService.kt](https://github.com/AutoAccountingOrg/AutoAccounting/blob/34b38f82ca9ad6d7905c715c66891977f8809c88/app/src/main/java/net/ankio/auto/service/NotificationService.kt) | title、bigText/text 抽取、allowlist、内容 hash 和重连调用 | 参考抽取顺序；钱本先做 allowlist，不沿用内容 hash 作为跨来源事实去重 |
| [无感记账 listener](https://github.com/DykiSensei/seamless-bookkeeping/blob/af717f9faa5658b2e67dcc61e200867951806734/app/src/main/java/com/bookkeeping/app/notification/PaymentNotificationListenerService.kt) | listener 调 parser，IO 协程调用 repository；debug 分支打印正文 | 参考模块分离；钱本 debug/release 都不自动记录正文 |
| [无感记账 repository](https://github.com/DykiSensei/seamless-bookkeeping/blob/af717f9faa5658b2e67dcc61e200867951806734/app/src/main/java/com/bookkeeping/app/data/repository/TransactionRepository.kt) | 默认 60 秒窗口，查询同来源同金额后决定跳过入库 | 这是可能误删连续真实交易的反例；时间窗只用于候选搜索 |
| [简记账 README](https://github.com/jinyule/bookkeeping/blob/e09c8eae9be239d647c49c1d1b851ef24cd792ff/README.md) | 自述白名单、待确认、本地 Room、CSV/manual、权限和验证材料边界 | 借鉴候选与确认分离；本次未完成该项目源码级采集路径审查 |
| [FinArt Android FAQ](https://finart.app/faq.html) | 作者说明短信与银行 App 通知等可作自动化入口 | 用于渠道互补参考，不证明中国银行/支付 App 的通知行为 |

源码 SHA：AutoAccounting `34b38f82ca9ad6d7905c715c66891977f8809c88`；无感记账 `af717f9faa5658b2e67dcc61e200867951806734`；简记账 README `e09c8eae9be239d647c49c1d1b851ef24cd792ff`。只阅读与总结采集机制，本次没有移植同行代码或规则包。

## 2. 官方约束与钱本设计

| 官方材料 | 核验事实 | 钱本据此作出的设计选择 |
|---|---|---|
| [NotificationListenerService](https://developer.android.com/reference/android/service/notification/NotificationListenerService) | Android N 起回调在主线程；连接后才能扫描 active；断开期间不接收事件 | 快照后 IO 持久化；连接时扫描当前通知、记录 coverage gap；不能声称恢复完整历史 |
| [Android Notification](https://developer.android.com/reference/android/app/Notification) | 提供 title/text/bigText 等字段和 group summary 标记 | 保存独立字段，聚合通知不直接当一笔交易 |
| [Android 15 CDD 敏感通知保护](https://source.android.com/docs/compatibility/15/android-15-cdd#3_8_3_4_sensitive_notification_protection) | 普通 listener 有敏感内容遮蔽规则，列有指定例外 | 权限开启与字段可读分开衡量；银行支付具体遮蔽行为仍待实测 |
| [WorkManager unique work](https://developer.android.com/develop/background-work/background-tasks/persistent/how-to/manage-work) | KEEP 会忽略已有工作期间的新请求；APPEND_OR_REPLACE 有不同链依赖语义 | Room 是待上传权威，调度是可补偿唤醒；防落盘后未调度及尾部新任务漏唤醒 |
| [PostgreSQL 显式锁](https://www.postgresql.org/docs/current/explicit-locking.html) | 行锁模式及竞争/死锁机制 | 领域 Ledger 独占锁、接收短共享锁、统一 Ledger→Job 锁顺序 |
| [PostgreSQL 约束](https://www.postgresql.org/docs/current/ddl-constraints.html) | 普通 CHECK 不承担跨行借贷汇总 | 受控账务入口＋延迟约束触发器，零行 journal 也校验 |
| [PostgreSQL 函数](https://www.postgresql.org/docs/current/sql-createfunction.html) | SECURITY DEFINER 的 search_path、执行权限需要收紧 | 受限 owner、限定 schema、PUBLIC 禁止执行、服务角色不能直接写历史账务 |
| [Go 事务](https://go.dev/doc/database/execute-transactions)、[pgx](https://pkg.go.dev/github.com/jackc/pgx/v5)、[sqlc WithTx](https://docs.sqlc.dev/en/latest/howto/transactions.html) | 显式事务和绑定事务的查询方式 | CommandService 统一 tx；Repository 不混用 pool 查询、不自行提交 |

右列是架构推导，不能当成同行已验证的实现。通知完整覆盖仍须账单/手动 ground truth 支持。

## 3. 自审发现及修复

P0：可能造成漏证据、错误账务、重复副作用或删除后再写入。P1：影响可重建性、可解释性、可用性或验证可信度。状态“已修订”仅指设计，不代表实现验证通过。

| ID | 级别 | v0.1 缺口与具体触发 | v0.2 修复位置与处理 |
|---|---|---|---|
| A-01 | P0 | Room 成功后进程退出，未 enqueue，交易一直留在本机 | §5.4：Room Queue 为权威，启动/重连/刷新和有待传时的周期补偿；覆盖 KEEP 尾部竞争 |
| A-02 | P0 | 服务端保存证据并 ACK 后崩溃，解析任务从未创建 | §5.5：Delivery、Observation、EvidenceReceived outbox 同事务，提交后 ACK |
| A-03 | P0 | 相同内容通知跨生命周期被 hash 合并；通知消失误被当成撤销 | §5：持久 episode 与来源快照身份；删除只是展示生命周期变化；不按时间＋金额删除证据 |
| A-04 | P1 | listener 授权成功即视为采集正常，忽略遮蔽/断线/聚合/资料隔离 | §5.2–5.3：content_availability、coverage_gap、连接恢复和场景覆盖矩阵 |
| A-05 | P0 | 旧 worker 租约失效但已执行写账，只在 ACK 时才被拒绝 | §8、§10：领域副作用及投影发布事务内验证 token；领取先提交，锁顺序固定 |
| A-06 | P1 | 自动采集无关交易提高 ledger_version，用户确认另一个事件也收到 409 | §8、§11：普通操作验 affected entity revisions，批量修复另验全局基准 |
| A-07 | P0 | A merge/split 后退款仍引用 A，或同一端转账重复闭合 | §8：关系修订/重定向/金额分配、退款总额及转账端占用检查，与修账原子提交 |
| A-08 | P1 | 跨日转账两端被整体排除后，日报无法解释银行余额减少 | §10：外部净流、内部现金变化、未分类净流分别列示并与现金变化闭合 |
| A-09 | P1 | staging generation 如何约束正式账务可见性没有合同 | §9：MVP 有界初始化原子事务，超限保持未初始化；大重建不宣称已支持 |
| A-10 | P1 | 最新 current 指针无法独立恢复过去 V；重放使用最新规则改变历史 | §10–11：committed_ledger_version、ChangeSet 指针及版本化关系；回放固定原政策 |
| A-11 | P0 | 删除开始后，已在途 ingestion 又插入证据/创建任务 | §5.5、§12：接收同事务短共享锁＋状态检查，删除独占锁；领域/投影再次校验状态 |
| A-12 | P0 | 只监听明细插入校验平衡，零行 journal 或旧 journal 追加行绕过 | §4：journal 自身延迟检查、不可变已提交内容、提交函数权限和冲销对称性 |
| A-13 | P1 | 来自微信的聊天金额被误当支付；调试日志暴露私人正文 | §5.2、§5.5：模板门控、默认排除非财务、debug 也脱敏；客户端结果不能直接授权入账 |

以上 13 项已写进 v0.2。设计仍需要 DDL 将约束具体化；仅写“使用锁”不足以证明协议成立。

## 4. 下一轮验证应证明什么

| 验证对象 | 必须出现的失败/边界 | 通过标准 |
|---|---|---|
| 真实通知 | 无系统通知、遮蔽、锁屏、聚合、分身、两笔同额、更新、重启 | 每场景有 ground truth 和字段样本；遗漏明确报告，不能称完整覆盖 |
| 本机队列 | 落盘后未 enqueue、尾部入队、上传后 ACK 前死亡 | 可恢复待发送；重试同 delivery；ACK 后不再生成新证据 |
| 后端接收 | 提交前/后故障、版本争抢、部分成功 | 不 ACK 未提交；已有证据有可靠解析任务；重复返回同 ID |
| 会计事务 | 同时修订、merge/split 任一步失败、退款超额、旧 journal 追加行 | 整体回滚或唯一完整结果，无重复效果，无越权写入 |
| 租约 | 旧 worker 卡住到租约过期，新 worker 提交后旧 worker 恢复 | 旧 token 在副作用事务内失败，不能写账或倒退投影 |
| 投影 | 跨日转账、迟到退款、分类修改、重复/乱序任务、重建 | 同 source_version 结果一致；现金变动公式成立；三视图同步发布 |
| 初始化/删除 | 账户未初始化、超限计划、删除与在途接收/发布竞争 | 无半初始化账务；无删除后新接受事实，备份恢复应用删除记录 |

纯文档阶段没有运行这些测试；工程阶段使用真实 PostgreSQL，通知使用真实 Android/OEM，不用仅模拟回调的测试宣称通知可行。

## 5. 当前结论与剩余交付

可以继续做 Go 工程和数据库实现设计。需交付的具体实现合同为：完整 DDL/权限/提交函数、Android payload schema、领域命令前置条件、OpenAPI、notification spike 记录及故障测试。无需改变已确认的三个产品决策：未知用途先暂记、MVP 固定资产原值、退款按发生月份冲减。

当前限制：通知覆盖没有实测；API P95、耗电、事务上限和恢复目标没有测量值；基础安全、删除与备份仍是设计要求。没有将这些未完成项标为已验收。
