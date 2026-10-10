# API 0.1

所有 `/v1` 请求必须使用 `Authorization: Bearer <token>`。身份来自令牌，账本权限来自服务端，客户端不能指定 actor。金额 JSON 为整数**字符串**，例如 `"2350"`，单位分。时间为 RFC3339；数据库精度为微秒。错误返回 `{code,message}`，内部异常不包含证据正文。

0.1.7 事件 Facts 可附 `foreign`：`original_amount` 为正十进制**字符串**（至多 12 位整数、6 位小数，无指数/分数表达式），`original_currency` 为用户声明的三位非 CNY 代码，`cny_settlement_confirmed` 表示用户是否核实最终 CNY 金额。未确认时 `amount_minor` 必须为 `"0"`，服务端将 currency 规范为 UNKNOWN 并保持 CNY_SETTLEMENT_REQUIRED，不得把估算值作为已结算金额；此时可先留空实际账户和发生时间。确认时须有正 CNY 分金额，再按通常账户/时间规则决定入账。

可选参考值 `reference_rate,reference_source,reference_at` 必须同时完整提供；它们是用户提供的参考信息，不被用于计算分录。结算确认后服务端根据 `(CNY分金额/100)/原币金额` 推导 `derived_rate`（12 位小数的近似显示）、精确的 `derived_rate_numerator/derived_rate_denominator` 与 `rate_source=SETTLEMENT_DERIVED_USER_CONFIRMED`，忽略客户端伪造的推导值或来源。修改原币元数据而 CNY 实际金额/账户/时间/性质不变时不重记；撤回确认并把 amount_minor 归零会原子冲销。每次接受的原币/结算信息仍保存在不可变修订和确认快照中。原币元数据不构成外币账户、报价服务或已验证银行结算声明。

## 路由

Android 0.1.4 新采集使用 `structured` 通知格式：`version=local-notification-v1`、字符串分金额 `amount_minor`、`currency=CNY|UNKNOWN`、`kind=UNKNOWN|CASH_IN|CASH_OUT`、64 位小写 SHA-256 `raw_hash`。`text`/`big_text` 必须为空；标题为支付固定服务名或银行来源标签。结构化内容是本地抽取提示，不证明实际账户、经济发生时间、用途或对账状态。客户端保留来源对象、快照号及采集/通知时间；服务端保存结构化不可变证据和解析器版本。未知版本或混入正文拒绝。旧版已排队通知仍兼容原格式，应先升级服务端再升级手机。

`POST .../funding` 请求含 `command_id`、`expected_version`（GET 返回的账本版本）、`rail=WECHAT|ALIPAY|UNIONPAY`、`account_id`、RFC3339 `effective_from`、可选 `effective_to` 及 `note`。空账户表示明确停用偏好。记录只追加；同一通道同一生效时点不可重复，重复 command_id 原内容重试返回原结果。某时点采用不晚于它的最新生效记录；该记录停用或到期时返回无候选，不恢复旧记录。偏好变化不生成分录。

`GET .../funding?rail=WECHAT&at=...&event_id=...` 参数均可选，返回 `source_version`、最近 200 条 `preferences`、`candidate` 和该事件的 `proofs`。候选明确标记 `proves_actual_funding=false`。新 `POST .../events` 手动确认在同一事务记录 USER_CONFIRMED 资金账户和还款目标关系，绑定修订与原始确认 observation；改选账户不覆盖旧依据。TRANSACTION_EVIDENCE 类型保留为独立类型，目前没有可信通知模板自动写入入口，也不允许客户端在偏好接口冒充该类型。旧版确认没有被伪造回填。

Android 0.1.11 将 allowlist 财务通知原文另存于本机 Keystore AES-GCM 加密的 Room v4 表，不进入上传 payload。默认保留 7 天，可选不保留、1/7/30 天；缩短期限同时缩短既有记录，延长不恢复已删除记录，也不延长既有到期日。全机最多 5000 份，超限移除最旧记录。读取时严格排除已到期数据；启动、新采集及独立无网络日任务执行到期 SQL 删除，Android 后台调度延迟时物理清理可能迟于到期，但应用不再返回到期原文。

设置可查看当前服务地址/令牌身份和账本的最近 50 份原文，并在本机重新解析成提示，不上传、不自动修改事实或账务。上传 ACK 不删除独立存档；删除本机原文不删除上传队列或服务器事实。删除账本同时删除该账本本机原文；选择不保留或手动删除清除全机原文。SQL 删除为逻辑删除，不宣称介质擦除、独立密钥销毁或备份删除。原文关闭保留、到期或删除后不能重新解析；原快照摘要哈希不能还原或独立证明原文。服务端结构化事实与账务历史仍可回放。服务器必要抽取片段和用户授权诊断上传仍待补齐。

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | `/health` | 进程存活检查 |
| GET / POST | `/v1/ledgers` | 列出 / 创建账本 |
| GET / POST | `/v1/ledgers/{id}/accounts` | 账户余额 / 创建账户 |
| POST | `.../opening` | 确认统一时点的期初 |
| GET / POST | `.../events` | 事件列表 / 确认或修订 |
| POST | `.../observations` | 分项持久化 ACK 的通知批次 |
| GET / POST | `.../funding` | 时间化支付偏好、仅候选的查询和事件资金来源确认依据 |
| POST | `.../csv` | 有明确账户的 CSV 原子导入 |
| GET | `.../evidence?event_id=...` | 原始版本化证据 |
| GET | `.../history?event_id=...&before_revision=...` | 每页 100 次修订及分录，游标为上一页最后修订号，排除该号；首请求省略游标 |
| GET | `.../relations?event_id=...` | 当前经济与身份关系 |
| POST | `.../merge` / `.../split` | 原子修复事件身份及账务 |
| POST | `.../transfer` / `.../unlink-transfer` | 关联 / 解除转账两端 |
| POST | `.../reimbursement` | 原子替换报销分配，空分配解除 |
| GET | `.../reimbursement-balances` | 逐笔垫付/回款的已分配及剩余金额 |
| GET | `.../duplicate-candidates?event_id=...` | 重复候选、理由及搜索范围 |
| GET | `.../transfer-candidates?event_id=...` | 现金转账两端候选，仅人工核实关联 |
| GET | `.../quality` | 同版本处理统计及待确认事件年龄 |
| GET | `.../reports?from=...&to=...` | 同版本三套视图，左闭右开时间范围 |
| GET | `.../assets` | 用户确认的固定资产购置与退款后原值 |
| GET | `.../rules` | 商户分类规则及失效历史 |
| POST | `.../revoke-rule` | 固定 command_id、rule_id、expected_version 撤销规则 |
| GET / POST | `.../balance-checks` | 最近 100 条余额检查 / 保存同一时点比较 |
| POST | `.../delete` | 输入 `confirm_name` 后删除该账本 |

事件列表每页 500 条，按创建时间及 ID 倒序。`?before=<上一页最后一个事件 ID>` 读取更早记录。

history 返回修订的 facts、evidence_ids、policy_version、source_version、created_at 和 journals。每个 journal 含 kind、effective_at、reversal_of、reversed_by、entries（账户名称及分的整数字符串）。修订产生的旧分录冲销属于该修订的 journals；旧正常分录通过 reversed_by 指向后续冲销。账户名称为当前显示名称，不是保存的历史名称。不存在事件返回 404，跨账本权限由服务端校验。

报表 `core-v2` 新增 `assets_minor`、`liabilities_minor`、`net_worth_minor`（均截至排他的 to 时点，包含期初及系统暂记账户）、`income_minor`、`surplus_minor`（收入减会计费用）及 `categories:[{category,consumption_minor}]`。分类合计等于生活消费；未填分类归入“未分类”，退款按其发生月和接受的分类冲减，可为负值。余额是账面值，不表示银行对账或资产完整录入。所有金额仍为分的整数字符串。

规则撤销不修改已接受事件，不自动恢复旧规则；版本过期或已失效返回 409，同一成功 command 重试返回原结果。

余额检查 POST 参数为 `command_id,account_id,as_of,actual_minor`。检查时间须在启用时点至当前时间内；余额为分的整数字符串，信用卡以欠款为正。返回 `id,account_id,as_of,actual_minor,book_minor,difference_minor,source_version`；差额为实际减账面，账面取 effective_at <= as_of 的全部分录，包括期初。仅保存比较，不写 Journal。source_version 是记录写入后的账本版本，之后新增或修订流水可能使历史结果过时。差额为零也不表示外部账单覆盖完整。

## 写命令

除初建账本和删除外，写命令包含 `command_id`，8–128 字符。推荐每次用户动作固定一个 UUID，并将完整请求持久化后再发送。相同命令同内容返回原结果；相同命令不同内容返回 409。事件、期初使用 `expected_revision`，并发旧版本返回 409。

账本创建：

```json
{"id":"客户端生成的 UUID","name":"我的账本","cutover_time":"2026-10-01T00:00:00+08:00"}
```

账户创建：

```json
{"command_id":"固定 UUID","name":"银行卡","type":"ASSET","cash":true,"provider":"机构名称","masked_ref":"1234"}
```

信用卡为 `LIABILITY`、`cash:false`。出资必须使用实际用户账户，系统会计科目不能作为实际账户。

期初：`command_id,account_id,expected_revision,amount_minor,as_of,meaning`。资产 `meaning:BALANCE`，负债 `meaning:DEBT`。`as_of` 必须等于账本启用时间；只初始化一次。

事件确认：

```json
{
  "command_id":"固定 UUID",
  "event_id":"事件 UUID",
  "expected_revision":0,
  "learn_category":false,
  "facts":{
    "kind":"EXPENSE","amount_minor":"2350","currency":"CNY",
    "occurred_at":"2026-10-02T12:30:00+08:00","time_precision":"EXACT",
    "funding_account_id":"实际账户 UUID","merchant":"商户","category":"餐饮","note":""
  }
}
```

另外支持 `INCOME,CASH_OUT,CASH_IN,TRANSFER_OUT,TRANSFER_IN,ADVANCE,REIMBURSEMENT,ASSET_PURCHASE,REFUND,ASSET_REFUND,CARD_REPAYMENT`。资产需要 `asset_title`；退款需要 `original_event_id`，历史消费退款须关联保存的有效历史原消费，`historical_original:true` 仅为声明，不能单独证明退款；还款需要 `repayment_account_id`。证据不足可返回 `REVIEW_REQUIRED`，这不等于已记成消费。待查款可 `posted:true`，但仍需确认用途。

学习分类只在商户与分类均明确时接受，仅影响之后收到的匹配证据；不会推断实际付款账户，也不会重写历史。

合并：`command_id,source_id,target_id,source_revision,target_revision`，保留 target 当前解释与汇集证据。拆分：`command_id,source_id,expected_revision,children:[Facts,...]`，2–20 个子事件，金额合计等于原金额，子事件须有完整入账信息。失败整体回滚。

转账关联：`command_id,out_id,in_id,out_revision,in_revision`。两端已正式入账、不同实际账户、等额、类型分别为转出和转入。关联仅添加关系。解除：`command_id,relation_id`。

通知批次：`{items:[Delivery,...]}`，1–50 条。字段见 Go `domain.Delivery`。必须使用明确允许的 package；`source_identity=android.notification:<package>`。source object key 是持续保存的 episode ID，snapshot key 标识 episode 内的变化序号，不是全文永久哈希。

通知 ACK 为数组，每项 `delivery_id,status,observation_id?,reason?`。只有 `ACK` 或 `IGNORED` 才删除客户端队列项；`REJECTED` 保留供检查。Observation 与 `EVIDENCE_RECEIVED` Outbox 在同一事务提交，提交后才 ACK。capture time 和 notification posted time 都不会冒充经济发生时间。

报表同时返回 `source_version,algorithm_version`，会计费用、生活消费、现金变动、外部/内部/未分类现金净额、待查余额和未关联转账计数。正式余额以全部 Journal/Entry 的累计为准；报表更正保留旧分录的现实时间。

## PRD 补齐中的接口变化

分类、商户、备注和资产名称单独修改保存新 EventRevision 与确认依据，但沿用原分录。history 的 `accounting_revision` 指向沿用的会计修订；该次 `journals:[]` 不表示原事件未入账。会计性质、金额、实际账户或经济时间修改仍冲销重记。分类报表读取当前接受的分类。

报表算法 `core-v3` 增加 `confirmed_assets_minor,confirmed_liabilities_minor,confirmed_net_worth_minor,provisional_net_worth_minor`。前两项分别排除待查资产、待查负债；后两项之和等于账面净资产。“已确认”表示用途与科目已确认，并非银行已对账。在途款仍包含在账面资产中，单独返回 transfer_clearing_minor。

0.1.6 报表算法 `core-v4` 新增 `pending_transfer_cash_minor`（待匹配净额）、`pending_transfer_in_minor`/`pending_transfer_out_minor`（分别汇总转入/转出）和 `card_repayment_cash_minor`（信用卡还款现金净额，通常为负）。未关联两端不能当成内部现金；即使关系已确认，期末另一端尚未发生也保持待匹配。净额为零不代表已闭合，应结合转入/转出及未关联端数查看。信用卡还款属于 external_cash_minor，不重复计消费。cash_change_minor 始终反映区间现金类账户账面变化。

转账候选要求目标与另一端均已入账、CNY 等额、反方向、不同实际账户、明确 EXACT 发生时间、没有已占用的经济关系；时间相距不超过七天，最多返回最近 500 条合格候选并披露截断及多候选歧义。响应沿用 candidate envelope 的 `event,reasons,target_revision,source_version`，算法 `transfer-review-v1`。候选不证明同一次划转，不能自动关联；人工确认使用原有 `POST .../transfer` 的两端修订号，原子关系闭合不生成第三套分录。可信交易引用及证据驱动自动闭合尚待补齐。

补期初在同一事务重检并激活该账户因未初始化而阻塞的已接受流水。任何校验失败回滚期初与整批激活；经济时间不晚于启用时间的流水不重复入账。

报销分配请求：`command_id,reimbursement_id,expected_revision,allocations:[{advance_id,expected_revision,amount_minor}]`，最多 50 项。允许部分分配；同一垫付不能重复，累计分配不得超过垫付，整批合计不得超过本笔回款。替换旧分配保留关系历史，失败不影响旧分配。关联不生成分录，回款和垫付的经济字段须先解除分配再修改。

重复候选只提示人工核实，检查最近最多 500 个同额事件，并返回 `search_truncated`。币种、方向、实际资金账户或同来源不同对象冲突排除候选；已拆分事件共享继承证据不会被重新建议合并。未取得经济时间时，可用采集时间生成候选，但不会将其作为入账依据。多个候选标记 ambiguous，合并仍需版本校验和显式确认。真实 PostgreSQL 查询及合并后完整簇约束已验收。

quality 返回已入账、自动入账、带手动/CSV 依据的事件数及待确认原因。手动与 CSV 依据可能重叠；自动入账不等于用途已确认、采集完整或银行已对账。异常年龄基于首次 event_created_at，而非经济时间或连续等待时间。账户另返回 book_as_of（最后分录经济时间）、evidence_received_at（最近相关证据收到时间）、last_balance_check_at（最近余额比较时点）；三者不能互相代替。

## 历史消费分析

`GET /v1/ledgers/{ledger}/historical-analysis?from=RFC3339&to=RFC3339` 在一致性快照读取当前历史事件，返回 source_version、algorithm_version=historical-v2、cutover_time 和 [from,to) 区间。只统计 `HISTORICAL_ONLY` 且发生时间不晚于起点的当前有效修订，合并/拆分终止事件不计入。

`gross_consumption_minor` 为 CNY EXPENSE 与 ASSET_PURCHASE 的退款前消费总額；categories 是扣除有效关联退款后的分类。included_count 为计入笔数；excluded_count 和 excluded 按 kind 列出其余历史事件的笔数与 CNY 金额。转账、垫付、待查款及未关联历史退款不算消费；`refund_minor` 为已建立 REFUND_OF 关系的区间内历史退款；`net_consumption_minor` 为原消费减关联退款，分类按原消费当前分类冲减。退款发生区间可以只有退款而呈负数，符合 D-03。未关联退款仍在 excluded。历史金额不是正式会计分录，不改期初、现金流或净资产，不证明账单完整或正式对账。

## 个人支付账单预览（适配进行中）

`POST /v1/ledgers/{ledger}/csv-preview` 接受 `{format,content}`，支持明确表头契约 `WECHAT_PERSONAL_V1` / `ALIPAY_PERSONAL_V1`。返回 native-preview-v1、逐行交易号/来源时间/金额/交易对方/支付方式/原始类型/状态/方向和支付方式列表。来源无时区的时间按该契约的 UTC+08:00 解释，不等同经过银行核验的经济时间。预览不写证据、事件、分录或版本，不等同导入。

只接受有效 UTF-8、800 KB/2000 行；最多在前 60 条 CSV 记录寻找精确且无重复的字段集。保留前言允许，未识别表尾、额外列、重复/缺失交易号、无效金额或时间会拒绝。金额按十进制整数分解析，拒绝浮点指数、负数和非两位小数。未知收支方向保留 UNKNOWN，原始交易状态不被强制解释为完成。不会读取对方账号/备注，也不推断实际资金账户。

微信表头字段集：交易时间,交易类型,交易对方,商品,收/支,金额(元),支付方式,当前状态,交易单号,商户单号,备注。
支付宝表头字段集：交易时间,交易分类,交易对方,对方账号,商品说明,收/支,金额,收/付款方式,交易状态,交易订单号,商家订单号,备注。

这两个契约只经合成样例验证，尚未用用户当前真实导出核对，不宣称覆盖所有版本或证明来源真实性。预览结果不是已导入；账户映射和确认写入由下述 `/csv-native` 完成。Android 设置页有独立支付账单入口，现有 `/csv` 和“导入 CSV”仍只接受钱本标准格式。

## 个人支付账单确认导入

`POST /v1/ledgers/{ledger}/csv-native`：`command_id`、`statement_account_id`、`format`、`content`、`confirmed_final_cny`、`payment_method_accounts`（原始支付方式字符串 → 当前账本实际账户 UUID）。来源账户用于同一支付账单来源的稳定去重，不能替代逐支付方式资金账户映射；同一来源始终选择同一账户。

明确完成状态目前限微信“支付成功/已收钱”和支付宝“交易成功”。只有用户确认实际 CNY 且为 IN/OUT、支付方式已显式映射时生成 CASH_IN/CASH_OUT，更新账户但用途仍待查。其余行保存为 UNKNOWN，不正式入账；起点前记录保持历史分析，不再次影响期初。取消/处理中/已退款原订单不自动记为完成消费。所有映射账户必须属于当前账本，不能使用系统科目。

复用标准 CSV 的原子事务、命令收据、不可变证据和行链接。来源身份为 csv.native:{format}:{statement_account_id}，对象键为交易号；规范化来源行和用户确认事实均保留。相同文件/映射重试去重，相同交易号内容或映射不同则整批冲突回滚，不自动覆盖旧事实。后续变动需从原事件修订；原生账单状态更新自动追加观察版本仍待实现。未知格式拒绝，当前两种契约仍仅经合成文件验证，不宣称真实渠道覆盖或已对账。无数据库迁移。
