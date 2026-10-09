# 钱本 0.1 开发版运行手册

本版本提供 Go API、事务 worker 和 Android 调试 App。正式记账主路径是手动确认与明确格式的 CSV；通知采集是实验入口，尚未完成实际银行、微信、支付宝的真机覆盖验收。

0.1.7 配套更新后端和 APK，不增加数据库迁移。“记账/确认事件 → 记录外币原始金额”保存原币代码和金额；尚无最终人民币结算时不勾选结算确认，记录进入待确认，可先留空实际账户和发生时间。核对最终 CNY 金额、账户和时间后勾选确认再保存。结算推导汇率来自用户确认值，不是外部报价；参考汇率不能触发入账。修正原币元数据而不改变 CNY 账务时沿用旧分录；撤回结算确认会原子冲销旧账并回到待结算。仍仅维护 CNY 账户，不支持外币账户持仓。

0.1.6 配套更新后端和 APK，数据库仍为迁移 001–008。首页现金流明细分别显示待匹配转入、转出和信用卡还款，未闭合转账不提前算内部现金。转入/转出事件的“更多操作 → 查看转账匹配候选”从服务端查询未占用的另一端，人工核实后关联；金额相同不能作为唯一依据。跨日到账在另一端尚未发生的报表期末仍显示在途；解除关联后重新进入待匹配。

0.1.5 升级时先停旧 API/worker，管理员运行 `migrate`（新增 008_funding_relations），再启动配套后端并安装 APK。“设置 → 管理支付偏好”记录通道偏好的生效变化，必须明确选择账户或停用；偏好仅是候选，不会自动扣款。新手动确认会保存逐修订的资金账户/还款目标依据，在事件“更多操作 → 查看资金来源依据”查看。旧确认不伪造回填，原证据仍在修订历史中。强交易证据的实际账户识别与冲突处理尚待实现。Android Room 仍为 v3。

0.1.3 需要配套更新后端和 APK；后端没有新增迁移，Android Room 自动升级至 v3 并保留原队列。分类/商户/备注修订不再冲销重记。“待确认 → 更多操作 → 分配报销回款”可部分分配到多笔垫付；留空后保存解除本笔回款分配。重复候选只提示人工核实。“设置 → 查看数据质量”和账户页分别展示处理依据及三个不同时间口径。

冷启动先显示可用的加密缓存，再后台刷新；成功写入后刷新当前月份报表缓存。缓存提示可点击刷新，缓存不代表已同步或银行对账。服务端返回 401/403 时清理该身份的视图缓存；切换服务或令牌清空旧账本内存视图。本轮设备覆盖是 API 35 模拟器，用户提供的 vivo X90s / Android 16 尚待连接验收。

0.1.2 更换原生界面并增加历史 API，不增加数据库迁移。使用配套后端和 APK；“待确认 → 更多操作 → 查看修订历史”查看最近 100 次接受的修订、旧账冲销和新账务。“设置 → 选择通知来源”选择允许新采集的来源，关闭不清空已有上传队列。主题跟随 Android 系统深浅色。

0.1.1 升级时先停旧 API/worker，管理员运行 `migrate`（新增 007_balance_checks），再启动新后端并安装新 APK，保留原令牌。首页支持月份切换及期末账面资产/负债/净资产；“设置 → 管理商户分类规则”可查看和撤销；“账户 → 检查实际余额”按指定时点比较并保留差额记录。信用卡输入实际欠款，欠款为正。余额检查不自动调整账务，旧检查结果不会跟随后续修订重算。

## 环境

- Go 1.24 及以上、Docker Desktop（Linux containers）。
- Android 构建：JDK 17、Android SDK 35；工程自带 Gradle 8.14.5 Wrapper。
- Android 8.0 / API 26 及以上。本次仪器测试使用 Android 15 / API 35 模拟器。

依赖版本锁在 `backend/go.mod`、`backend/go.sum` 和 Android Gradle 文件。Java 下载依赖可能需要显式设置 Gradle 的 HTTP/HTTPS 代理；勿将代理凭据提交到仓库。

## Windows 快速启动

从仓库根目录运行：

```powershell
.\scripts\dev.ps1 init
```

这会创建本机 PostgreSQL、应用有校验和的迁移，并创建一个用户。保存终端中仅显示一次的访问令牌及用户 ID。`init` 再执行会再创建一个用户；恢复已有用户的访问权请使用下文 `token USER_ID`。

两个终端分别运行：

```powershell
.\scripts\dev.ps1 api
```

```powershell
.\scripts\dev.ps1 worker
```

API 默认只监听 `127.0.0.1:8080`，数据库只发布 `127.0.0.1:15432`。Compose 的账户与密码**仅适用于本地开发**。API/worker 启动时检查数据库角色，拒绝使用管理员角色。

`.env.example` 是配置示例，程序读取进程环境变量，不会自动加载 `.env`。使用其他数据库时先明确设置 `DATABASE_URL`；生产需要数据库 TLS、HTTPS 入口、不同的强密码、受控网络与进程管理。

## Android

构建前配置 JDK 与 SDK 的实际安装目录，例如：

```powershell
$env:JAVA_HOME='C:\Program Files\Java\jdk-17'
$env:ANDROID_HOME="$env:LOCALAPPDATA\Android\Sdk"
```

```powershell
.\scripts\dev.ps1 apk
adb install -r android/app/build/outputs/apk/debug/app-debug.apk
adb reverse tcp:8080 tcp:8080
```

App 中填 `http://127.0.0.1:8080` 和刚生成的访问令牌。`adb reverse` 适用于连接到开发电脑的设备或模拟器。Release 构建禁止明文 HTTP；调试构建仅供本机开发使用，不发布为正式发行包。

1. 创建账本，确认统一启用时间。
2. 添加实际银行卡、钱包余额或信用卡，分别确认同一时点的期初余额或欠款。
3. 在“记账”输入以元为单位的金额，选择实际付款/到账账户及发生时间。
4. 用“待确认”查看原始证据、修订分类和解释；修改已入账事件会自动冲销旧分录。
5. 退款选择原消费；退款在实际发生月份冲减消费。原消费在启用时间前时，可明确勾选历史原消费。
6. 转账分别确认转出与转入，再关联两端；信用卡还款选择银行和目标信用卡。
7. 在“设置”导入 CSV、查看固定资产原值或删除账本。删除要求输入完整账本名称。

通知采集默认关闭，需要同时开启 App 开关与系统通知使用权。系统重连只能读取仍在显示的通知；通知移除仅关闭采集 episode。微信/支付宝额外限制财务服务标题，防止将普通聊天作为财务证据。系统隐藏或不符合模板的内容无法解析，不代表交易不存在。金额、实际账户或时间不足时保持待确认。

通知与写操作在发送前持久化；正文、令牌和命令正文由 Android Keystore 的 AES-GCM 密钥加密。WorkManager 自动重试；“重试上传”可手动触发。写操作尚未确认时，阻止新写操作抢先提交。变更服务地址后，旧队列不会发送至新地址。服务器拒绝的队列项保留在本地，设置页显示计数；修订冲突需刷新后重新确认。

## CSV 合同

UTF-8，可含 BOM，表头必须为：

```csv
record_id,occurred_at,amount_minor,kind,merchant,category
record-001,2026-10-02T12:30:00+08:00,2350,EXPENSE,示例食堂,餐饮
```

- `record_id`：该实际账户中的稳定流水 ID。相同 ID 内容不同会拒绝导入，不以商户/金额/时间猜测重复。
- `occurred_at`：含时区的 RFC3339 发生时间。
- `amount_minor`：**正整数分**。这里与 App 的“元”输入不同，文件金额不使用浮点数。
- 类型：`EXPENSE`、`INCOME`、`CASH_OUT`、`CASH_IN`、`TRANSFER_OUT`、`TRANSFER_IN`、`ADVANCE`、`REIMBURSEMENT`。
- 退款/资产先导入为待查款，再确认用途。系统不声称此格式兼容任何银行原生 CSV。
- 单文件最多 800 KB、2000 行，整体事务提交。相同文件或稳定流水 ID 的重复导入不会重复记账。
- 启用时间及以前的流水仅保存历史证据，不再改变期初之后的正式余额。

合成样例见 `examples/statement.csv`。如果账本启用时间晚于样例日期，这些行会标记为历史，不会影响余额。

## 检查与维护

```powershell
.\scripts\dev.ps1 test
cd android
.\gradlew.bat :app:lintDebug :app:connectedDebugAndroidTest
```

Go 集成测试必须提供 `TEST_ADMIN_DATABASE_URL` 和 `TEST_DATABASE_URL`，否则会明确跳过真实数据库检查。测试脚本设置本地 Compose 对应地址。Android 仪器测试需要设备或模拟器连接。

CLI 在 `backend` 目录执行，管理员 `DATABASE_URL` 只用于迁移和令牌管理：

```text
go run ./cmd/qianben migrate
go run ./cmd/qianben user
go run ./cmd/qianben token USER_ID
go run ./cmd/qianben revoke
```

`revoke` 从 `QIANBEN_TOKEN` 环境变量读取待撤销令牌。新令牌归属同一个用户时，原账本仍可访问；不要通过创建新用户来模拟令牌轮换。

迁移由数据库事务、advisory lock 和 SHA-256 校验和保护。已登记的迁移不可修改，后续变更添加新 SQL 文件。兼容开发早期已执行 `001_core.sql` 但没有迁移表的本地实例；正常新实例会完整执行所有迁移。

## 备份与恢复

以下在本地开发实例使用，备份包含敏感财务数据与令牌哈希，需存放在仓库忽略的私有目录并保护访问：

```powershell
docker exec qianben-postgres-1 pg_dump -U qianben_admin -d qianben -Fc -f /tmp/qianben.dump
docker cp qianben-postgres-1:/tmp/qianben.dump ./private-data/qianben.dump
```

恢复应在独立数据库中验证，确认后才切换服务；所需数据库角色由管理员预建。本机 Compose 中这些角色已存在：

```powershell
docker exec qianben-postgres-1 createdb -U qianben_admin qianben_restore
docker exec qianben-postgres-1 pg_restore -U qianben_admin --exit-on-error -d qianben_restore /tmp/qianben.dump
```

三套报表直接读取同一 PostgreSQL 快照中的不可变分录与账本版本，无需重新调用模型或解析器。恢复时必须核对恢复版本、余额及借贷平衡。删除账本不等于擦除过去的备份；恢复旧备份前应按外部保留的删除记录再次执行删除，并撤销不应继续有效的旧令牌。

## 当前边界

- 自动跨来源识别只收集证据；确认同一事件由用户合并，不使用同金额时间窗口自动去重。
- 带既存退款/划转经济关系的事件合并、拆分或核心财务字段修改会受保护。先修订退款、解除转账关联，再执行修复；自动分摊既存经济关系后移。
- 报销到账使用独立模板；尚未自动匹配垫付批次，也不做银行正式对账。
- 固定资产只覆盖用户确认的购置与退款后的原值，折旧、处置、估值后移。
- 通知银行覆盖、真机耗电、厂商后台限制、敏感内容遮蔽仍需真实设备验证。本机测试结果不代表生产错误率为零。
