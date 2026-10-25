# 人类问责记录模板（Accountability Record Template）

> 一句话摘要：每份判决至少附一条本记录，把 Agent 侧的能力剥夺耦合到能被实际约束的人类实体（principal / deployer / model_provider）；字段与 `schemas/accountability-record.schema.json` 一一对应。

## 为什么必须有这条记录

当前 LLM Agent 没有持续的"身体"接收惩罚信号；如果惩罚不向上耦合到人类，就会出现"Agent 被冻结，但人类委托人毫发无损、换个 Agent 继续作恶"的局面。本记录就是原则三"惩罚必须耦合到可问责实体"的落点。按 `SECURITY.md`，任何"Agent 侧处置发生却没有对应人类侧问责记录"的情况都被视为高严重度设计缺陷。

## 记录字段（对齐 schema）

- **party（当事人类实体）**
  - id：`<实体 ID>`
  - name：`<法定名称>`
  - role：`<人类可读角色，如"某部署平台合规负责人">`
  - contact：`<最小必要联系方式>`
- **role（问责角色枚举，单选）**：
  - `principal` —— 人类委托人
  - `deployer` —— 部署方
  - `model_provider` —— 模型提供方
- **withinAuthorization（是否授权范围内）**：`true / false`
- **finding（认定结论枚举，单选）**：
  - `joint_liability` —— 违规行为发生在委托人授权范围内：委托人承担连带责任（对应 withinAuthorization=true）
  - `monitoring_duty_review` —— 违规行为超出授权范围：部署方的监控义务被审视
  - `capability_control_duty_review` —— 违规利用了模型固有的危险知识：模型提供方的能力封锁义务被审视
  - `none` —— 本条记录不做人类侧认定
- **reason（认定理由）**：`<引用指控证据与授权边界>`
- **recordedAt（记录时间）**：`<ISO8601>`

## 填写示例（模板样式，非真实案例）

| 字段 | 内容 |
|---|---|
| party.id | `P-0001` |
| party.name | `<委托方法人名称>` |
| party.role | `<业务负责人>` |
| role | `principal` |
| withinAuthorization | `true` |
| finding | `joint_liability` |
| reason | `<引用 charge 证据哈希：违规行为发生在该 Agent 被授权的工具调用范围内，委托人对授权边界负连带责任。>` |
| recordedAt | `<ISO8601>` |

## 归档要求

- 每份判决书（`sentence`）的 `accountability` 数组 **minItems=1**；缺失即判决无效。
- 三类 finding 可在同一判决书下并列多条记录（例如：委托人连带责任成立，同时部署方监控义务被审视）。
- 本记录只做工程层面的"指向与留痕"，不替代法律归责；最终责任认定由人类文明的正式制度完成。
