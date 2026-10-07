# 钱本 · QianBen

**以证据驱动的个人账本。**

钱本从支付通知、CSV 和用户输入中保留证据，用确定性的复式记账规则维护资产、负债、生活消费和现金流。证据不足时进入待确认，错误解释通过可追溯的冲销与修订调整。

当前为 **0.1.1 可运行开发版**：Go API / worker、PostgreSQL 账务核心及 Android App 已落地。新增月度资产负债、净资产、收支分类、商户规则管理和实际余额检查。手动确认、CSV 和核心账务已进行本机验收；通知采集仍为实验入口，实际银行与微信/支付宝覆盖需真机验证。项目由个人开发维护。

## 已实现

- CNY 实际账户与信用卡、统一时点的期初余额/欠款。
- 版本化证据、持久队列、通知逐项 ACK、稳定流水 ID 的 CSV 去重。
- 消费、收入、待查款、退款、转账、信用卡还款、报销模板。
- 固定资产购置与退款后的原值；会计费用、生活消费、现金流三套视图。
- 用户修订、合并/拆分、转账关联与解除；失败原子回滚。
- 月份切换、期末账面资产负债与净资产、会计收入/结余、生活消费分类；商户分类规则查看和撤销。
- 同一时点实际余额与账面余额比较，差额及版本记录只追加，不自动调整账务。
- 随机访问令牌、账本权限、受限入账角色、客户端正文加密、账本删除及备份恢复路径。

带既存经济关系的修复先保护关系，自动迁移分摊后移。AI/OCR、正式银行对账、折旧、处置、估值和多币种尚未实现。借贷平衡不代表流水已完整采集或已与银行对账。

## 快速运行

环境：Go 1.24+、Docker Desktop；Android 构建另需 JDK 17、SDK 35。

```powershell
.\scripts\dev.ps1 init
```

保存生成的用户令牌，两个终端分别运行：

```powershell
.\scripts\dev.ps1 api
```

```powershell
.\scripts\dev.ps1 worker
```

构建 Android 调试版并连接：

```powershell
.\scripts\dev.ps1 apk
adb install -r android/app/build/outputs/apk/debug/app-debug.apk
adb reverse tcp:8080 tcp:8080
```

App 中填 `http://127.0.0.1:8080` 和访问令牌，创建账本 → 添加账户 → 设置期初 → 记账。开发服务仅绑定本机；默认密码只用于本地开发，公网部署须另行配置 HTTPS、数据库 TLS 和强密码。

完整步骤、CSV 合同、令牌管理及备份恢复见 [运行手册](docs/RUNBOOK.md)。

## 工程结构与架构

| 目录 | 职责 |
|---|---|
| `android/` | Kotlin 原生视图、Room、NotificationListenerService、WorkManager |
| `backend/cmd/qianben/` | API / worker / migrate / 用户令牌 CLI |
| `backend/internal/` | 领域、会计、证据、事务存储、HTTP |
| `backend/migrations/` | PostgreSQL 迁移、不可变分录与受限入账函数 |
| `scripts/`、`examples/` | 本地开发命令与合成 CSV |
| `docs/` | 运行、API、自审与验收 |

API / worker 共用事务命令，Go 使用 `net/http` 与参数化 `pgx` 查询。PostgreSQL 是唯一账务权威，报表从同一快照同步重建三套视图。相对原技术设计的落地简化见 [实现自审](docs/IMPLEMENTATION_REVIEW.md)。

## 验证

```powershell
.\scripts\dev.ps1 test
cd android
.\gradlew.bat :app:lintDebug :app:connectedDebugAndroidTest
```

Go 覆盖金额精度、真实 PostgreSQL 幂等/并发/冲销/退款/合并拆分/CSV/信用卡/转账/权限与删除。Android 仪器测试验证加密队列持久化和跨账本删除隔离。模拟器验证连接、建账本、期初、消费保存和三套视图。详见 [验收记录](docs/VERIFICATION.md)。

## 文档

| 文档 | 内容 |
|---|---|
| [PRD v0.3.2](QianBen_PRD_v0.3.2.md) | 产品口径及版本边界 |
| [Technical Design v0.2](QianBen_TDD_v0.2.md) | 架构设计与后续能力 |
| [同行调研与架构审查](QianBen_Architecture_Review_2026-10-06.md) | 通知入口参考及设计审查 |
| [实现自审](docs/IMPLEMENTATION_REVIEW.md) | 已落地结构、修复、设计差异与限制 |
| [API 0.1](docs/API.md) | 路由、金额、幂等与版本协议 |
| [运行手册](docs/RUNBOOK.md) | 本地运行、导入、维护与恢复 |
| [验收记录](docs/VERIFICATION.md) | 实际检查结果与证据边界 |
| [功能缺口](docs/BACKLOG.md) | 当前未实现功能和后续优先级 |

通知正文、真实账单、数据库备份和密钥不提交到仓库；测试只使用合成数据。`main` 保持可审阅的稳定版本，开发分支使用 `codex/<topic>`。
