# API 0.1

所有 `/v1` 请求必须使用 `Authorization: Bearer <token>`。身份来自令牌，账本权限来自服务端，客户端不能指定 actor。金额 JSON 为整数**字符串**，例如 `"2350"`，单位分。时间为 RFC3339；数据库精度为微秒。错误返回 `{code,message}`，内部异常不包含证据正文。

## 路由

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | `/health` | 进程存活检查 |
| GET / POST | `/v1/ledgers` | 列出 / 创建账本 |
| GET / POST | `/v1/ledgers/{id}/accounts` | 账户余额 / 创建账户 |
| POST | `.../opening` | 确认统一时点的期初 |
| GET / POST | `.../events` | 事件列表 / 确认或修订 |
| POST | `.../observations` | 分项持久化 ACK 的通知批次 |
| POST | `.../csv` | 有明确账户的 CSV 原子导入 |
| GET | `.../evidence?event_id=...` | 原始版本化证据 |
| GET | `.../relations?event_id=...` | 当前经济与身份关系 |
| POST | `.../merge` / `.../split` | 原子修复事件身份及账务 |
| POST | `.../transfer` / `.../unlink-transfer` | 关联 / 解除转账两端 |
| GET | `.../reports?from=...&to=...` | 同版本三套视图，左闭右开时间范围 |
| GET | `.../assets` | 用户确认的固定资产购置与退款后原值 |
| POST | `.../delete` | 输入 `confirm_name` 后删除该账本 |

事件列表每页 500 条，按创建时间及 ID 倒序。`?before=<上一页最后一个事件 ID>` 读取更早记录。

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
