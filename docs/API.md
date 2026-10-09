# API 0.1

所有 `/v1` 请求必须使用 `Authorization: Bearer <token>`。身份来自令牌，账本权限来自服务端，客户端不能指定 actor。金额 JSON 为整数**字符串**，例如 `"2350"`，单位分。时间为 RFC3339；数据库精度为微秒。错误返回 `{code,message}`，内部异常不包含证据正文。

## 路由

Android 0.1.4 新采集使用 `structured` 通知格式：`version=local-notification-v1`、字符串分金额 `amount_minor`、`currency=CNY|UNKNOWN`、`kind=UNKNOWN|CASH_IN|CASH_OUT`、64 位小写 SHA-256 `raw_hash`。`text`/`big_text` 必须为空；标题为支付固定服务名或银行来源标签。结构化内容是本地抽取提示，不证明实际账户、经济发生时间、用途或对账状态。客户端保留来源对象、快照号及采集/通知时间；服务端保存结构化不可变证据和解析器版本。未知版本或混入正文拒绝。旧版已排队通知仍兼容原格式，应先升级服务端再升级手机。

`POST .../funding` 请求含 `command_id`、`expected_version`（GET 返回的账本版本）、`rail=WECHAT|ALIPAY|UNIONPAY`、`account_id`、RFC3339 `effective_from`、可选 `effective_to` 及 `note`。空账户表示明确停用偏好。记录只追加；同一通道同一生效时点不可重复，重复 command_id 原内容重试返回原结果。某时点采用不晚于它的最新生效记录；该记录停用或到期时返回无候选，不恢复旧记录。偏好变化不生成分录。

`GET .../funding?rail=WECHAT&at=...&event_id=...` 参数均可选，返回 `source_version`、最近 200 条 `preferences`、`candidate` 和该事件的 `proofs`。候选明确标记 `proves_actual_funding=false`。新 `POST .../events` 手动确认在同一事务记录 USER_CONFIRMED 资金账户和还款目标关系，绑定修订与原始确认 observation；改选账户不覆盖旧依据。TRANSACTION_EVIDENCE 类型保留为独立类型，目前没有可信通知模板自动写入入口，也不允许客户端在偏好接口冒充该类型。旧版确认没有被伪造回填。

当前新采集不持久化通知全文，因此不能对原文重新解析；摘要哈希只能标识原快照，不能还原或独立证明原文。结构化事实与账务历史可查看回放。原始抽取片段、本地原文保留期限和用户授权诊断上传仍须补齐。

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

另外支持 `INCOME,CASH_OUT,CASH_IN,TRANSFER_OUT,TRANSFER_IN,ADVANCE,REIMBURSEMENT,ASSET_PURCHASE,REFUND,ASSET_REFUND,CARD_REPAYMENT`。资产需要 `asset_title`；退款需要 `original_event_id`，历史消费退款可明确 `historical_original:true`；还款需要 `repayment_account_id`。证据不足可返回 `REVIEW_REQUIRED`，这不等于已记成消费。待查款可 `posted:true`，但仍需确认用途。

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
