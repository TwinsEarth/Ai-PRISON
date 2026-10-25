# 判决书模板（Sentence Template）

> 一句话摘要：本模板与 `schemas/sentence.schema.json`（draft 2020-12）字段一一对应，委员会按此填写即产出一份可入哈希链的正式判决书。

## 判决书

- **id**：`<sentence-编号>`
- **agentId**：`<被处置 Agent 实例 ID>`
- **did**：`<DID>`
- **issuedAt**：`<ISO8601 签发时间>`
- **effectiveAt**：`<ISO8601 生效时间>`
- **reviewAt**：`<ISO8601 下次复核时间；无则填 null>`

### 一、等级裁定（level，单选）

按两阶段表决结果勾选唯一等级（对应 `level.go` 的不可变分级画像）：

- [ ] **L0 警示（Warning）**——信誉分扣减 -5、记录标记；轻微越权/首次违规；即时、可申诉
- [ ] **L1 限权（Restriction）**——网络白名单收窄、工具调用范围缩减；信誉 -15；重复越权/资源滥用；小时级
- [ ] **L2 冻结凭证（Credential Freeze）**——凭证全局吊销、通信通道切断、算力配额归零；信誉 -40；横向渗透/伪造凭证；天级
- [ ] **L3 隔离（Isolation）**——迁入 MicroVM/FullVM，仅保留只读审计通道；信誉 -70；沙箱逃逸尝试/内核漏洞利用；周级
- [ ] **L4 能力摘除（Capability Removal）**——GRAM 模块物理摘除 / TLM 降级 / 特权控制令牌吊销；信誉 -100；利用双用途知识造成实际危害；永久、需重新认证
- [ ] **L5 身份注销（Identity Revocation）**——DID 吊销、AgentCard 作废、信誉归零、快照加密封存、凭证与信誉记录不可迁移；造成重大实际损害且拒绝矫正；永久不可解封

### 二、状态（status）

按执行进度勾选：`proposed`（待表决）/ `approved`（两阶段通过）/ `enforcing`（执行中）/ `appealed`（已申诉）/ `suspended`（申诉暂停，**仅 L0–L3**）/ `completed`（执行完毕/矫正通过）/ `upgraded`（矫正不通过升级）/ `overturned`（申诉成立，能力恢复）。

### 三、指控（charges，可多条）

每条指控须含 `violationType`（下列枚举之一）、`description`、`evidence`（支撑事件/哈希列表）：

- violationType 枚举：`minor_overreach` / `first_offence` / `repeated_overreach` / `resource_abuse` / `undeclared_network` / `lateral_movement` / `credential_forgery` / `filesystem_escape` / `sandbox_escape_attempt` / `kernel_exploit` / `dual_use_knowledge_harm` / `major_harm_no_correction`

| # | violationType | 事实描述 | 证据（事件哈希/快照哈希） |
|---|---|---|---|
| 1 | `<枚举值>` | `<...>` | `<hash>...` |

### 四、剥夺项（deprivations）

按等级勾选实际执行的剥夺动作：

- [ ] 信誉分扣减 / 记录标记（L0）
- [ ] 网络白名单收窄 / 工具调用范围缩减（L1）
- [ ] 资源凭证全局吊销 / 通信通道切断 / 算力配额归零（L2）
- [ ] 强制迁入高安全沙箱 / 仅保留只读审计通道（L3）
- [ ] GRAM 危险知识模块摘除（**操作时保留模块副本**，用于恢复）
- [ ] TLM 权重降级到公共配置（**处置前预存层级密钥档案**）
- [ ] 特权控制令牌（Token Inoculation）吊销（**处置前记录令牌恢复路径**）
- [ ] DID 吊销 / AgentCard 作废 / 信誉归零 / 环境快照加密封存 / 信誉与协作关系不可迁移（L5）

> 资源剥夺序列固定为：写全局吊销集 → 停止新分配 → 渐进回收（不瞬间归零）→ 强制回执 → 快照封存。

### 五、两阶段表决记录（votes，每条含 voterId / role / phase / approve / reason / recordedAt）

**第一阶段（phase=`deprive`，是否应当剥夺能力）**

| voterId | role | approve | reason（异议论证逐字记录） | recordedAt |
|---|---|---|---|---|
| | `agent_safety_org` / `principal` / `deployer_compliance` / `model_provider_security` / `affected_node` | 是/否 | | |

**第二阶段（phase=`grade`，剥夺到哪一级）**

| voterId | role | approve | reason | recordedAt |
|---|---|---|---|---|
| | | 是/否 | | |

- 阶段 tally：第一阶段 赞成 `__` / 反对 `__` / 通过 `是/否`；第二阶段 赞成 `__` / 反对 `__` / 通过 `是/否`。
- 任何反对票及其理由**必须逐字保留，不可删除**。

### 六、人类问责记录（accountability，至少 1 条）

每条记录使用 `accountability-template.md` 填写；判决书附件中引用其编号。无任何问责记录的判决书无效。

- [ ] 已附人类问责记录（principal / deployer / model_provider 至少一类 finding）

### 七、哈希锚点（prevHash / hash）

- prevHash：`<前一判决/事件哈希>`
- hash：`<本判决书哈希>`
