# 实现追溯矩阵（Design → Implementation Traceability）

> 本表把源设计文档的每一条核心要求，追溯到它在 Go 代码里的具体落点：**包/文件 → 关键类型或函数 → 所在版本 → 实现性质**。
> 目的是让评审者一眼看清「哪条设计是真的在跑、哪条只是接口/模拟占位」，并在 v1.0.0 做一次文档/Schema/代码一致性收口。
>
> **实现性质图例**
> - **内存实现(真实逻辑)**：状态机、哈希链、配额阶梯、表决、回执、撤销集等是本仓库里真实可跑的逻辑（只是跑在内存里，不连真实系统）。
> - **接口占位/模拟**：GRAM / Token Inoculation / TLM / 硬件绑定 / MicroVM / 快照托管 是**抽象接口 + 内存模拟后端**，不是真实管控；代码注释与文档均如实标注 `simulated`。

---

## 1. 六条技术路径

| 设计需求（技术路径） | Go 包 / 文件 | 关键类型 / 函数 | 版本 | 实现性质 |
|---|---|---|---|---|
| 沙箱隔离（Wasm→Container→MicroVM→FullVM 梯度） | `internal/enforcement/isolation/` | `Gradient`、`Controller.Establish/ConfirmReady/Live`、`backends.go`（backend 标签） | v0.5 | 接口占位/模拟（backend 仅标签；无真实 Wasmtime/Docker/Firecracker） |
| 资源剥夺（吊销→停新分配→渐进回收→快照） | `internal/enforcement/resource/` | `Engine.Deprive`、`InMemoryBackend.Allocate/PlanReclaim/ReclaimStep`、`DefaultSteps` | v0.4 | 内存实现(真实逻辑)（cgroup/配额本身是模拟计数） |
| 权限/凭证冻结（全局撤销集优先于过期） | `internal/revocation/`、`internal/domain/credential.go` | `Set.IsRevoked/RevokeCredential`、`Credential.IsUsable`（先撤销后过期） | v0.2 | 内存实现(真实逻辑) |
| 通信切断（强制回执、未回执节点连带冻结） | `internal/enforcement/identity/` | `Service.CutCommunication/AckNode`、`summarizeLocked`（group-freeze） | v0.6 | 内存实现(真实逻辑) |
| 迁移抑制（硬件绑定/记录密钥/快照托管） | `internal/enforcement/migration/` | `Service.SuppressMigration`、`MemHardwareBinder`、`MemRecordKeeper`、`MemCustodyStore` | v0.8 | 接口占位/模拟（无真实 TPM/硬件根、无真实多方托管） |
| 身份吊销（DID/AgentCard 作废、信誉归零、快照封存） | `internal/enforcement/identity/` | `Service.RevokeIdentity`、`AgentCard.Revoked`、`ValidateAccountability` | v0.6 | 内存实现(真实逻辑) |

## 2. 执行层五大能力

| 执行能力 | Go 包 / 文件 | 关键类型 / 函数 | 版本 | 实现性质 |
|---|---|---|---|---|
| 资源剥夺引擎 | `internal/enforcement/resource/` | `Engine`、`DepriveInput/Report`、`MemSnapshotStore` | v0.4 | 内存实现(真实逻辑) |
| 隔离梯度 + 行为监控 | `internal/enforcement/isolation/` | `Gradient`、`Controller`、`Monitor.Observe`（进行中冻结） | v0.5 | 接口占位/模拟（backend/监控信号为标签） |
| 身份与通信 + 人类问责 | `internal/enforcement/identity/` | `Service`、`DetermineAccountability`、`InMemoryTopology` | v0.6 | 内存实现(真实逻辑) |
| 迁移抑制 | `internal/enforcement/migration/` | `Service`、`SuppressInput`、硬件绑定/记录密钥/托管三措施 | v0.8 | 接口占位/模拟 |
| 能力封锁（GRAM/Token/TLM） | `internal/enforcement/capability/` | `Service.Block/Recover`、`GramRegistry`、`TokenGate`、`TLModel` | v0.9 / v1.0.0 恢复 | 接口占位/模拟（无真实权重/神经元/令牌栈；恢复入口 v1.0.0） |

## 3. L0–L5 每一级

| 等级 | 名称 | 权威画像来源 | 执行落点（编排层 `internal/prison/pipeline.go`） | 版本 | 实现性质 |
|---|---|---|---|---|---|
| L0 | 警示 Warning | `level.go` `levelProfiles[L0]` | 仅信誉扣减（`domain.ApplyReputation`） | v0.1 | 内存实现(真实逻辑) |
| L1 | 限权 Restriction | `level.go` `levelProfiles[L1]` | 状态 `StatusRestricted` | v0.1 | 内存实现(真实逻辑) |
| L2 | 冻结凭证 Credential Freeze | `level.go` `levelProfiles[L2]` | 通信切断 + 资源渐进剥夺；申诉/期满可恢复 | v0.4/v0.6；演练 v1.0.0 | 内存实现(真实逻辑) |
| L3 | 隔离 Isolation | `level.go` `levelProfiles[L3]` | 新建 MicroVM + 只读审计 + 迁移抑制 + 行为监控；矫正评估可恢复 | v0.5/v0.7；演练 v1.0.0 | 内存实现(真实逻辑)（MicroVM 为标签） |
| L4 | 能力摘除 Capability Removal | `level.go` `levelProfiles[L4]`（`RequiresRecertification=true`） | `Capability.Block`；恢复经 `Capability.Recover` + 重新认证 | v0.9；恢复入口 v1.0.0 | 接口占位/模拟（GRAM/TLM/Token 为模拟） |
| L5 | 身份注销 Identity Revocation | `level.go` `levelProfiles[L5]`（`Recoverable=false`） | `Identity.RevokeIdentity` + 快照托管 + 记录密钥吊销；**不可恢复** | v0.6/v0.8；性质演练 v1.0.0 | 内存实现(真实逻辑) |

## 4. 四大产品原则

| 原则 | 代码落点 | 关键函数 / 字段 | 版本 | 实现性质 |
|---|---|---|---|---|
| ① 惩罚能力，不惩罚感受 | `internal/domain/` + 执行层各剥夺动作 | `LevelProfile.Deprivations`、各 `Deprive/Block/Cut` | v0.1–v0.9 | 内存实现(真实逻辑) |
| ② 可逆但有成本 | `internal/governance/` + `internal/prison/drill.go` | `CanRecover`、`RestorationOrder`、`Recover`（重新认证门槛） | v0.7；演练 v1.0.0 | 内存实现(真实逻辑) |
| ③ 耦合可问责人类 | `internal/domain/accountability.go` + `identity` | `AccountabilityRecord`、`ValidateAccountability`（minItems=1） | v0.6 | 内存实现(真实逻辑) |
| ④ 监狱不是目的，矫正才是 | `internal/governance/rehabilitation.go` | `EvaluateRehabilitation`（通过→恢复，失败→升级一级） | v0.7 | 内存实现(真实逻辑) |

## 5. 三大难题（迁移/重建不可根除）

| 难题 | 设计应对 | 代码落点 | 版本 | 实现性质 |
|---|---|---|---|---|
| 权重副本可在别处重训/重载 | 不删模型，而是抬高迁移与重建成本 | `migration` 全部措施 + L5 信誉不可继承 | v0.8 | 接口占位/模拟 |
| 「换个新 DID 从零开始」无法阻止 | L5 只保证原身份信誉/协作图不可继承，不保证阻止新建 DID | `MemRecordKeeper.Portable=false`、`AgentCard.Revoked` | v0.6/v0.8 | 内存实现(真实逻辑) |
| 隔离终将失效 | 纵深防御 + 行为监控进行中冻结 + 审计兜底 | `isolation.Monitor`、`audit.Chain` | v0.3/v0.5 | 内存实现(真实逻辑)（监控信号为模拟） |

## 6. 审计四要素

| 审计要素 | Go 包 / 文件 | 关键类型 / 函数 | 版本 | 实现性质 |
|---|---|---|---|---|
| 哈希链（tamper-evident judgement chain） | `internal/audit/chain.go` | `Chain.Append/Verify`（SHA-256 prevHash 链接） | v0.3 | 内存实现(真实逻辑) |
| 执行回执（forced receipts） | `internal/audit/receiptbook.go` | `ReceiptBook.RegisterPending/Ack`、`Receipt` | v0.3 | 内存实现(真实逻辑) |
| 端到端合规报告（per-hop vs e2e） | `internal/audit/compliance.go` + `pkg/contracts` | `Book.Report`、`contracts.ComplianceReport` | v0.3 | 内存实现(真实逻辑) |
| 快照证据链 | `internal/enforcement/resource/snapshot.go` + `migration/custody.go` | `MemSnapshotStore`、`SealedCustody`（M-of-N） | v0.4/v0.8 | 接口占位/模拟（托管密钥为模拟） |

## 7. 治理生命周期（七阶段 + 两阶段同意 + 申诉 + 矫正）

| 治理环节 | Go 包 / 文件 | 关键函数 | 版本 | 实现性质 |
|---|---|---|---|---|
| 七阶段裁定（立案→证据→基线→…） | `internal/governance/` | `OpenCase/AdmitEvidence/ConfirmBaseline/IssueSentence` | v0.7 | 内存实现(真实逻辑) |
| 两阶段同意（先剥夺？后分级？） | `internal/governance/` + `internal/domain/sentence.go` | `CompleteDeprive/CompleteGrade`、`VotePhase=deprive/grade` | v0.7 | 内存实现(真实逻辑) |
| 判决状态机 | `internal/governance/statemachine.go` + `domain/sentence.go` | `transition`、`SentenceStatus` 八态 | v0.7 | 内存实现(真实逻辑) |
| 申诉（仅委托人；L0–L3 暂停，L4–L5 不暂停） | `internal/governance/appeal.go` | `FileAppeal/RuleAppeal`、`AllowsAppealSuspension` | v0.7 | 内存实现(真实逻辑) |
| 矫正评估（通过恢复/失败升一级） | `internal/governance/rehabilitation.go` | `EvaluateRehabilitation` | v0.7 | 内存实现(真实逻辑) |
| 恢复演练矩阵自动化 | `internal/prison/drill.go` + `cmd/aiprison drill` | `RunRecoveryDrill`、`DrillReport` | v1.0.0 | 内存实现(真实逻辑) |

---

## 一致性声明（v1.0.0 收口）

- `internal/domain` 枚举（Level L0–L5、SentenceStatus、VotePhase、CommitteeRole、CredentialKind/State、ReceiptStatus、EventType、AccountabilityRole/Finding）与 `schemas/*.json` 逐项对齐。
- `docs/01` 与 `README.md` 的分级表与 `internal/domain/level.go` 的 `levelProfiles` 一致。
- GRAM / Token Inoculation / TLM / 硬件绑定 / MicroVM / 快照托管 在代码注释与本矩阵中均标注为**接口/模拟**；仓库不存在任何「已实现真实管控」的夸大表述，也不含任何逃逸/攻击/武器化/危险知识提取步骤。
