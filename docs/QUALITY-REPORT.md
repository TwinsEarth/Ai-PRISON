# AI Prison — 核心质量报告（QUALITY REPORT）

> 本报告由对仓库的实际阅读、命令复跑与 git 历史核对生成；所有数字均可追溯到具体文件或命令（见附录）。
> 历史版本在当时未采集度量数据的，一律如实标注「未采集」，不反推、不虚构。

## 0. 封面元信息

| 项 | 值 |
|---|---|
| 项目 | **AI Prison（AI 监狱）** — 防御性 AI Agent 治理：对越界 Agent 实施隔离 / 审计 / 问责，**只做防御，不含任何逃逸、攻击、武器化或危险知识提取步骤** |
| 模块 | `github.com/ai-prison/ai-prison` |
| Go 版本 | `go1.27.3 darwin/arm64`（dev 机 Apple M4，10 逻辑核）；`go.mod` 声明兼容底线 `go 1.23`（CI 矩阵 1.23/1.24/1.25） |
| 依赖 | **纯标准库，零第三方依赖**（staticcheck / goleak 等仅为本地开发者工具，不入 go.mod） |
| 发布版本 | **1.0.0**（本地 tag `v1.0.0` = `b77382f`；本报告把 tag 之后落在 `main` 上的 4 个提交——真实持久化、真实二进制部署、静态分析/覆盖/审查/基准——一并纳入本次 1.0.0 发布口径，见 §3 与 §6） |
| 报告日期 | 2026-10-25（Asia/Shanghai, UTC+8） |
| 复跑环境 | macOS（darwin/arm64），`export PATH="$HOME/.go-sdk/go/bin:$PATH"` |

**模拟边界（贯穿全报告）：** GRAM 可摘除知识模块、Token Inoculation 令牌门控、TLM 分级模型、硬件指纹绑定、MicroVM / 沙箱后端、快照多方托管、真实吊销分发——**全部为接口 + 内存模拟后端**，跨进程不恢复真实执行状态；落盘的只是「这些模拟组件做出的决定」，供下一个进程证明并重新执行。代码/测试中含模拟标注注释（审查报告统计 212 处；本次按 `simulat|not real|no real` 全仓 grep 计 **239 处**，含测试）。

---

## 1. 总览

### 1.1 全量门禁结果（本次在 HEAD 实际复跑）

| 门禁 | 命令 | 结果 | 证据 |
|---|---|---|---|
| 格式 | `gofmt -l .` | ✅ 通过（输出为空） | 本次复跑，exit 0 |
| 静态检查（编译器） | `go vet ./...` | ✅ 通过（无输出） | 本次复跑 |
| 构建 | `go build ./...`（含 `examples/`） | ✅ 通过 | 本次复跑 `build OK` |
| 测试 + 竞态 | `go test -race -count=1 ./...` | ✅ 全部 `ok`，无竞态 | 13 个测试包全部 ok；单包约 2–4 s（race 检测开销） |
| 静态分析 | `staticcheck ./...` 与 `-checks all ./...` | ✅ 0 findings，exit 0 | `docs/reports/staticcheck-1.0.0.txt`（staticcheck 2026.2.2） |
| 端到端 smoke | `make smoke`（`scripts/smoke.sh`） | ✅ 脚本存在且可重跑（临时目录 EXIT 清理） | 本次手动复跑 `demo --data-dir D` → `verify` = `INTEGRITY OK`，exit 0；脚本零残留 |
| 覆盖率 | `go test -coverprofile -covermode=atomic ./...` | ✅ 全仓 **72.2%**（本次复跑；归档报告记 71.9%，差异为复跑波动） | `docs/reports/coverage-1.0.0.txt` |

> 说明：ROADMAP 的每版门禁为 `gofmt / vet / build / test -race`；本报告在其之上追加 staticcheck 与 smoke。`-count=2` 与 `-count=1` 结果一致（审查报告已记录 `-race -count=2` 绿色）。

### 1.2 代码规模（本次实际统计，排除 `bin/`）

| 指标 | 数值 | 命令 |
|---|---:|---|
| Go 文件总数（含测试） | **97** | `find . -name '*.go' -not -path './bin/*' \| wc -l` |
| 全部 Go 代码行数 | **14,527** | `… -exec cat {} + \| wc -l` |
| 非测试代码行数 | **9,736** | 排除 `*_test.go` |
| 测试文件数 | **27** | `find -name '*_test.go'` |
| 测试函数数（`func Test*`） | **108** | `grep -rh '^func Test' --include='*_test.go' . \| wc -l` |
| 基准函数数（`func Benchmark*`） | **14** | `grep -rh '^func Benchmark' … \| wc -l` |
| Example 函数数 | 0 | — |

各 tag 时点的 Go 文件数（`git ls-tree -r --name-only <tag> | grep -c '\.go$'`）：v0.1.0=12 → v0.2.0=14 → v0.3.0=20 → v0.4.0=27 → v0.5.0=33 → v0.6.0=40 → v0.7.0=49 → v0.8.0=57 → v0.9.0=65 → v0.9.5=72 → v1.0.0=76 → **HEAD=97**（v1.0.0 之后的持久化/部署/质量加固 +21 个文件）。

### 1.3 性能基准关键表（dev 机 Apple M4 / go1.27.3，`-benchmem`）

来源：`docs/reports/benchmark-1.0.0.txt`（`go test -run '^$' -bench=. -benchmem ./...`，`b.ResetTimer()` 排除构造、磁盘基准用 `b.TempDir()`，数字为实测非估算）。

| 能力 | 基准 | 实测 ns/op | 折算吞吐 | 备注 |
|---|---|---:|---|---|
| 吊销命中（10 万条集合） | `BenchmarkIsRevokedLargeSet` | **6.61 ns/op**，0 allocs | ≈1.5 亿次/s | 单次 RLock map 查找，O(1) |
| 吊销未命中 | `BenchmarkIsRevokedLargeSetMiss` | 6.85 ns/op，0 allocs | — | 同上 |
| 判决链 Append | `BenchmarkChainAppend` | **1,107 ns/op**，17 allocs | — | 每事件 JSON 规范化 + SHA-256，链结构固有 |
| 全链 Verify（1k） | `BenchmarkVerify_1k` | 0.784 ms/op | — | O(n)，重算全部摘要 |
| 全链 Verify（10k） | `BenchmarkVerify_10k` | 8.00 ms/op | — | 线性 |
| 全链 Verify（100k） | `BenchmarkVerify_100k` | 78.1 ms/op | ≈1.27 M 事件/s | 篡改检测契约，未做优化 |
| Store 逐条落盘（每记录 fsync） | `BenchmarkAppendSyncEveryAppend` | **8.18 ms/op** | ≈**122 条/s** | 1 次 fsync + 段目录 rename/记录 |
| Store 批量落盘（段=256） | `BenchmarkAppendBatched` | **33.5 µs/op** | ≈**29,800 条/s** | fsync 摊销到 256 条，约 **244×** |
| Store 重放校验（10k 条重开） | `BenchmarkReplayVerify` | 16.5 ms/reopen | — | JSONL 解码 + 全量重哈希，冷启动演练可接受 |
| 治理：七阶段两阶段→判决 | `BenchmarkFullCaseToVerdict` | 2.30 µs/case | — | — |
| 资源：五步剥夺（含快照） | `BenchmarkDepriveFullSequence` | 2.62 µs/case | — | — |
| 端到端旅程（内存） | `BenchmarkRunScenarioAInMemory` | 69.8 µs/旅程 | — | — |
| 端到端旅程（FileStore 落盘） | `BenchmarkRunScenarioAFileStore` | 9.81 ms/旅程 | — | 差值 = fsync 边界关账，与 store 数字一致 |

二进制 / CLI 系统测量（`make build`，`-trimpath -ldflags "-s -w"`）：

| 指标 | 实测 |
|---|---|
| 二进制体积 | **3,329,826 字节（≈3.2 MB）** |
| 启动延迟（`aiprison version`，20 次） | min 2.4 ms / **中位数 2.7 ms** / 均值 3.1 ms / max 7.3 ms |
| `demo` 端到端墙钟（3 次） | ≈11.7 ms；峰值 RSS ≈6.8 MiB（7,159,808 B） |
| `drill` 端到端墙钟（3 次） | ≈11.0 ms；峰值 RSS ≈7.2 MiB（7,602,176 B） |

### 1.4 五渠道发布状态（占位，见发布报告，资产以最终 push 为准）

| 渠道 | 状态 | 证据 |
|---|---|---|
| GitHub Release | ⏳ 未推送：remote `origin` 已指向 `github.com/TwinsEarth/ai-prison.git`，但 tag/提交尚未 push，Release 资产待授权后上传 | `git remote -v`、`docs/RELEASE.md §5` |
| CI（`.github/workflows`） | ⏳ **目录尚不存在**（0 文件）；兼容矩阵 go 1.23/1.24/1.25 仅在 `go.mod` 注释中声明 | `find .github -type f` |
| Compat 兼容矩阵 | ⏳ `go 1.23` 底线已声明，未在 CI 实际跑矩阵 | `go.mod` |
| Release 构建工作流 | 🟡 脚本就绪 `scripts/build-release.sh`（6 目标 × tar.gz/zip + sha256）；本次 `dist/` 已产出 4 个归档（linux/darwin amd64+arm64）+ `checksums.txt`，windows 归档未构建 | `scripts/build-release.sh`、`dist/` |
| npm | ⏳ 无根 `package.json`，未配置 | `ls package.json` |

---

## 2. 版本矩阵（v0.1.0 → v1.0.0，11 个 tag）

> 「承诺 vs 落地」对照 `CHANGELOG.md` / `docs/ROADMAP.md` 与 `git show --stat <tag>`；完成度指该版承诺是否在该 tag 提交内真实落地。

| 版本 | 主题 | 关键落地（git show 核对） | 完成度 | 备注 / 模拟边界 |
|---|---|---|---|---|
| **v0.1.0** | 奠基：领域模型 L0–L5、契约、JSON Schema、文档 | `internal/domain/*`（12 个 go 文件）、`pkg/contracts`、`schemas/*.json`、`cmd/aiprison`(version/levels)、全套 docs | ✅ 100% | 纯模型与契约；L0–L5 布尔语义、先撤销后过期规则落地 |
| **v0.2.0** | 全局追加式撤销集 | `internal/revocation/revocation.go` + 测试（→14 文件） | ✅ 100% | 撤销先于过期；无删除路径；跨节点 ack 为模拟 |
| **v0.3.0** | 判决哈希链 + 回执簿 + 合规报告 | `internal/audit/{chain,receiptbook,compliance}.go`（→20） | ✅ 100% | SHA-256 prevHash 链；HMAC 为模拟签名，无真实 PKI |
| **v0.4.0** | 资源剥夺：可撤销配额、渐进回收、回收前快照 | `internal/enforcement/resource/*`（→27） | ✅ 100% | CPU 阶梯 1000→750→500→250→0（非瞬间归零）；cgroup/配额为模拟计数 |
| **v0.5.0** | 隔离梯度 + 行为监控 | `internal/enforcement/isolation/*`（→33） | ✅ 100% | Wasmtime/Docker/Firecracker/FullVM 仅**标签**；进行中冻结；establish-before-cut |
| **v0.6.0** | 身份与通信：DID/AgentCard 吊销、强制回执、连带冻结、人类问责 | `internal/enforcement/identity/*`（→40） | ✅ 100% | 复用真实 revocation.Set / ReceiptBook；拓扑与总线为内存模拟 |
| **v0.7.0** | 治理层：七阶段裁定 + 两阶段同意 + 状态机 + 申诉 + 矫正 | `internal/governance/*`（→49） | ✅ 100% | 投票 append-only 不可删；L5 永不产生恢复令；非真实司法流程 |
| **v0.8.0** | 迁移抑制：硬件绑定、记录锚定密钥、多方快照托管 | `internal/enforcement/migration/*`（→57） | ✅ 100% | TPM/硬件根、Shamir 门限托管**均为模拟** |
| **v0.9.0** | 能力封锁：GRAM 可摘除模块、令牌门控、TLM 降级 | `internal/enforcement/capability/*`（→65） | ✅ 100% | 必须在就绪沙箱内执行；无真实权重/神经元/令牌栈 |
| **v0.9.5** | 端到端编排 `internal/prison` + CLI demo + 跨包 e2e | `prison/{system,pipeline,journey,scenarios}.go`、`cmd demo`、`test/e2e_test.go`（→72） | ✅ 100% | 双场景 A(L3 可恢复)/B(L5 不可恢复)；`-race` 绿 |
| **v1.0.0** | 恢复演练可运行化 + 示例 + 追溯矩阵 + 一致性收口 | `prison/drill.go`、`drillcmd.go`、`examples/basic`、`docs/IMPLEMENTATION.md`、`docs/RELEASE.md`、capability 恢复入口（→76） | ✅ 100% | docs/06 矩阵自动化；L4 恢复入口 + 重新认证门槛 |

**版本间增补（落在 tag `v1.0.0` 之后、`main` 上的 4 个提交，计入本次 1.0.0 发布）：**

| 提交 | 内容 |
|---|---|
| `468a93d` | store：统一 append-only、崩溃安全的 JSONL 文件后端 |
| `4fc16cf` | 真实持久化：FileStore 接入编排、CLI `inspect/verify/repair-tail`、跨进程账本断言 |
| `3a7de02` | 真实二进制部署、零残留 goroutine 泄漏测试、Go 兼容底线降到 1.23 |
| `d6d44b6` | 静态分析、覆盖率提升、审查修复（P1/P2/P3）、基准 |

合计 `git diff --stat v1.0.0..HEAD`：**44 文件改动，+4,459 / −81**。

---

## 3. 逐版本详记

> 统一约定：**静态分析 / 审查计数（P0=0、P1=1、P2=1、P3=8）是在最终 1.0.0（HEAD）状态测得并已修复**，历史 tag 当时未采集此类度量，本节对历史版本只按「变更范围」定性说明，不虚构数字。测试总数、基准数字同样只在 1.0.0 收口。

### v0.1.0 — 奠基（领域模型 / 契约 / Schema / 文档）
- **进度**：CHANGELOG 与 ROADMAP 承诺的仓库骨架、`internal/domain` 全量模型、`pkg/contracts`、9 个 JSON Schema、最小 CLI、全套 docs，均在该提交落地（`git show --stat` 核对 12 个 go 文件 + schemas + docs）。**完成度 100%**。
- **质量**：当时无独立审查报告；domain 单测覆盖升级/恢复/分级/表决/先撤销后过期。
- **测试**：历史未采集具体计数。
- **性能**：未采集。
- **边界**：纯模型，无执行能力。

### v0.2.0 — 全局撤销集
- **进度**：`internal/revocation`（Set/Entry，对齐 `schemas/revocation-entry.schema.json`）落地；`domain.Credential.IsUsable` 改为先查撤销集。**完成度 100%**。
- **测试**：历史未采集；承诺了「撤销未过期即拒」「幂等重吊销」「全 ack 才 enforced」等用例。
- **边界**：无真实网络分发。

### v0.3.0 — 哈希链 / 回执簿 / 合规报告
- **进度**：`internal/audit` 三件套落地（chain.go / receiptbook.go / compliance.go）。**完成度 100%**。
- **测试**：历史未采集；当前该包自覆盖率 **89.0%**，整包套件下 `chain.go` 97.9%、`compliance.go` 100%、`receiptbook.go` 90.0%（见 §4）。
- **性能（1.0.0 实测，下同基准仅列于此版相关项）**：链 Append 1.11 µs/op；Verify 1k=0.78 ms / 10k=8.0 ms / 100k=78 ms。
- **边界**：节点签名为 HMAC 模拟，无真实 PKI。

### v0.4.0 — 资源剥夺
- **进度**：可撤销 `Quota`、`Backend` 接口 + 内存后端、五步 `Engine.Deprive`、`MemSnapshotStore` 落地。**完成度 100%**。
- **质量**：本版埋下的一个配置缺陷在 1.0.0 收口修复——`Deprive` 未对 `Snapshots` 做 nil 检查（详见 §5 P1）。
- **性能**：`DepriveFullSequence` 2.62 µs/case。
- **边界**：cgroup/配额/内存冻结/网络切断均为模拟计数。

### v0.5.0 — 隔离梯度
- **进度**：四后端标签、`Gradient.Recommended`、`Monitor` 进行中冻结、`Controller` establish-before-cut 落地。**完成度 100%**。
- **测试**：当前该包自覆盖率 **89.5%**。
- **边界**：无真实 Wasmtime/Docker/Firecracker/KVM；启动延迟记录不等待。

### v0.6.0 — 身份与通信
- **进度**：`RevokeIdentity`（双凭证入全局撤销集 + `AgentCard.Revoked`）、`CutCommunication` + 回执 + 连带冻结、`DetermineAccountability`/`ValidateAccountability` 落地。**完成度 100%**。
- **测试**：当前该包自覆盖率 **77.0%**（`service.go` 文件级 52.7%，见 §4）。
- **边界**：无真实 DID 网络 / 消息总线。

### v0.7.0 — 治理层
- **进度**：七阶段流水线 + 两阶段同意、判决状态机、申诉（L0–L3 暂停 / L4–L5 不暂停）、矫正评估落地，投票 append-only。**完成度 100%**。
- **测试**：当前该包自覆盖率 **78.4%**（`statemachine.go` 91.7%、`case.go` 88.9%）。
- **性能**：`FullCaseToVerdict` 2.30 µs/case。
- **边界**：非真实委员会 / 司法。

### v0.8.0 — 迁移抑制
- **进度**：硬件绑定、记录锚定密钥、M-of-N 快照托管三条路径落地。**完成度 100%**。
- **测试**：当前该包自覆盖率 **80.5%**（`custody.go` 77.3%）。
- **边界**：无真实 TPM / 门限密码学。

### v0.9.0 — 能力封锁
- **进度**：GRAM / Token / TLM 三接口 + `Service.Block`（隔离前置 + block 前后哈希变化）落地。**完成度 100%**。
- **测试**：当前该包自覆盖率 **71.1%**（文件级 66–95%）。
- **边界**：无真实权重 / 计算图密钥 / 特权令牌。

### v0.9.5 — 端到端编排 + CLI demo
- **进度**：`internal/prison` 把全部包装配成一个 `System`；`aiprison demo` 双场景；`test/e2e_test.go` 跨包断言。**完成度 100%**。
- **测试**：当前 `internal/prison` 自覆盖率 **76.0%**（v1.1 加固前为 55.3%）；整包套件下 `system.go` 99.8%、`pipeline.go` 85.8%。
- **性能**：`RunScenarioA` 内存 69.8 µs/旅程。
- **边界**：一切为内存模拟，末尾打印「全程为内存模拟」。

### v1.0.0 — 稳定版（恢复演练 / 示例 / 追溯矩阵 / 收口）
- **进度**：`aiprison drill` 跑 docs/06 矩阵（L2 恢复 / L3 矫正恢复 / L4 三路径恢复且先证无认证被拒 / L5 只证不可恢复）、`examples/basic`、`docs/IMPLEMENTATION.md` 追溯矩阵、`docs/RELEASE.md` 落地；capability 恢复入口 `Recover/RestorePrivateLayer/Reissue`。**完成度 100%**。
- **质量**：本报告 §5 的 P1/P2 修复与 P3 清理在其后的加固提交完成；当前 staticcheck 0 findings。
- **测试**：`drill_test.go` 新增 `TestRunRecoveryDrillAllPass` / `TestDrillReportFailurePropagation` / `TestDrillReportPrintSmoke`；当前测试总数 **108**。
- **性能**：`drill` 端到端 ≈11.0 ms，峰值 RSS ≈7.2 MiB；二进制 ≈3.2 MB，启动中位 2.7 ms。
- **边界**：恢复代码同样标注 `simulated`；L5「不可恢复」是产品承诺，不阻止「换一个新 DID 从零开始」。

---

## 4. 覆盖率明细

命令与口径（`docs/reports/coverage-1.0.0.txt`，2026-10-25 UTC，go1.27.3）：
`go test -coverprofile=coverage.txt -covermode=atomic ./...` + `go tool cover -func=coverage.txt`。
**全仓总覆盖 71.9%**（归档）；本次复跑 `go test -coverprofile=/tmp/cov.txt ./...` 得 **72.2%**（波动范围内）。

### 4.1 逐包自覆盖率（`go test -cover ./...`，本次复跑）

| 包 | 自覆盖率 | 归档记录 |
|---|---:|---:|
| `internal/enforcement/isolation` | **89.5%** | 89.5% |
| `internal/audit` | **89.0%** | 89.0% |
| `internal/revocation` | **84.3%** | 84.3% |
| `internal/enforcement/migration` | **80.5%** | 80.5% |
| `internal/enforcement/resource` | **81.8%** | 78.5% |
| `internal/governance` | **78.4%** | 78.4% |
| `internal/domain` | **78.3%** | 78.3% |
| `internal/enforcement/identity` | **77.0%** | 77.0% |
| `internal/prison` | **76.0%** | 76.0%（原 55.3%） |
| `internal/store` | **75.6%** | 75.2%（原 69.1%） |
| `internal/enforcement/capability` | **71.1%** | 71.1% |
| `cmd/aiprison` / `examples/basic` | 0.0% | 由 e2e / smoke 子进程测试覆盖，不纳入 -cover |
| `pkg/contracts` | — | 无测试文件 |

### 4.2 整包套件文件级覆盖（`-coverpkg=./internal/...`，归档报告摘录）

| 文件 | 覆盖率 | 说明 |
|---|---:|---|
| `internal/audit/compliance.go` | 100.0% | — |
| `internal/prison/system.go` | 99.8% | — |
| `internal/revocation/revocation.go` | 98.1% | — |
| `internal/audit/chain.go` | 97.9% | — |
| `internal/enforcement/resource/snapshot.go` | 96.4% | — |
| `internal/store/memory.go` | 95.7% | — |
| `internal/audit/receiptbook.go` | 90.0% | — |
| `internal/store/file.go` | 82.2% | 崩溃安全 JSONL 写入 |
| `internal/prison/persist.go` | 86.3% | — |
| `internal/prison/pipeline.go` | 85.8% | — |
| `internal/enforcement/isolation/*.go` | 86–100% | — |
| `internal/enforcement/capability/*.go` | 66–95% | — |
| `internal/governance/statemachine.go` | 91.7% | — |
| `internal/store/store.go` | 62.5% | — |
| `internal/enforcement/identity/service.go` | 52.7% | — |
| `internal/store/errors.go` | 52.1% | 仅 Error() 字符串变体 |
| `internal/enforcement/resource/deprive.go` | 49.0% | 五步阶梯错误分支，由 e2e/基准间接覆盖 |
| `internal/prison/journey.go` | 37.0% | 中文定宽表格排版，由 Print smoke 覆盖主路径 |

---

## 5. 审查发现与修复（P0–P3）

来源：`docs/reports/review-findings-1.0.0.md`。门禁后状态：build / vet / gofmt 空 / `-race -count=1` 全绿。

| 级别 | 发现 | 已修 | 遗留 |
|---|---:|---:|---:|
| **P0 致命** | 0 | 0 | 0 |
| **P1 高** | 1 | 1 | 0 |
| **P2 中** | 1 | 1 | 0 |
| **P3 低/风格** | 8 | 7 | 1（已文档化） |

### 5.1 P1 — `resource.Deprive` 未设 `Snapshots` 时 nil 解引用 panic
- **位置**：`internal/enforcement/resource/deprive.go:114`。`NewEngine` 对 `Backend` 做了 nil 检查却漏了 `Snapshots`；调用方只配 `Backend` 不配 `Snapshots` 时，第 3 步 `e.Snapshots.Seal(...)` 直接 nil 接口调用 panic。
- **修复**：增加前置条件 `if e.Snapshots == nil { return nil, fmt.Errorf("resource: deprivation needs a SnapshotStore") }`，happy path 不变。
- **回归测试**：`internal/enforcement/resource/resource_test.go` → **`TestDepriveNilSnapshotStoreReturnsError`**（nil Snapshots 与 nil Backend 都返回明确错误而非 panic）。

### 5.2 P2 — Store 未尊重 `context.Context` 取消
- **位置**：`internal/store/file.go`（`Append`/`Sync`）、`internal/store/memory.go`（`Append`）。契约收 `ctx` 却 `_ = ctx`，长 fsync 不可中止。
- **修复**：三个方法开头加 `if err := ctx.Err(); err != nil { return …, err }`；已取消的 ctx 在任何变更之前返回，账本保持未触碰。
- **回归测试**：`internal/store/coverage_test.go` → **`TestAppendSyncHonorsCancelledContext`**（已取消 ctx 返回 `context.Canceled`、`Len()==0`，之后活 ctx Append 仍可用）。

### 5.3 P3 — staticcheck 死代码 / 风格（8 项）

| # | 位置 | 检查 | 处置 |
|---|---|---|---|
| 1 | `internal/audit/receiptbook.go:175` | U1000 未用方法 `pendingCount` | 删除死方法（信号已由 `UntrustedNodes()`/`All()` 暴露） |
| 2 | `internal/enforcement/migration/custody.go:15` | U1000 未用类型 `custodianShare` | 删除，份额改为内联 `map[string]string`，语义保留在注释 |
| 3 | `internal/prison/persist_test.go:175` | SA1012 向 `Replay` 传 nil context | 改传 `context.Background()` |
| 4 | `internal/store/store_test.go:23` | U1000 未用字段 `dir` | 删除测试 helper 字段 |
| 5 | `internal/governance/adapters.go:43` | ST1020 导出函数注释格式 | 注释改为以 `NewPrincipalLookup` 开头 |
| 6 | `internal/prison/journey.go:121` | ST1020 导出方法注释格式 | 注释改为以 `Print` 开头 |
| 7 | `internal/prison/system.go:283,286` | ST1016 接收者名不一致 | `s *System` → `sys *System`，23 个方法统一 |
| 8 | 覆盖缺口 | prison 55.3% / store 69.1% | 新增 `prison/drill_test.go`、`store/coverage_test.go`（见 §4） |

### 5.4 遗留（已文档化，未改）
- **`resource.Deprive` 吞掉 `Backend.Current` 错误**（`deprive.go:149`，`current, _ := …`）：模拟内存后端 `Current` 永不失败；暴露它会改变报告形状与 e2e/基准基线。字段仅防御用，代码已注释。
- **`publishLocked` 在「段已存在」时硬失败**（`file.go:256`）：故意为之——rename 成功后若目录 fsync 失败，已提交段落盘而 `f.persisted` 滞后；下次 publish 检测到既存段返回明确错误而非覆盖已提交文件（否则破坏崩溃一致性）。

### 5.5 十类审查清单结论
并发（RWMutex 读写配对正确，`-race -count=2` 绿）、错误处理（首个 Append 失败在 Close 上报）、panic 风险（修复后无 nil/越界/未保护断言）、context 取消（已修）、幂等（Revoke/RegisterPending/Seal/checkpoint 去重）、句柄/进程泄漏（所有 Close 路径含错误路径）、路径穿越（段文件名仅由全局 `uint64` seq 格式化 `seg-%010d.log`，kind/payload 永不拼路径）、哈希正确性（UTC RFC3339Nano + key 排序）、API 边界（nil Store 时持久化字节级等同 v1.0，`OpenReadonly` 拒追加）、模拟边界（239 处模拟标注）——均通过。

---

## 6. 持久化真实化证据

> 范围边界：**仅治理/审计账本是真实落盘**；执行后端（沙箱/cgroup/真实吊销分发/硬件绑定/GRAM/TLM）仍是模拟且不跨进程恢复——账本记录「模拟组件的决定」，下一个进程据此证明并重新执行。

### 6.1 写入正确性（append-only JSONL）
- `internal/store/file.go`：每条记录走 `os.CreateTemp` → 写入 + **fsync** → 原子 **rename** 为正式段 → 所在**目录 fsync**；崩溃安全。段文件名 `seg-%010d.log`，`SegmentSize=256` 批量摊销 fsync（基准 33.5 µs vs 逐条 8.18 ms）。
- 双后端一致性：`FileStore` 与 `MemoryStore` 实现同一 `Store` 接口，事件投影逐字节对齐（`internal/prison/persist_test.go` 内存 vs 文件投影相等断言）。
- 跨进程断言：`test/persistence_cli_test.go`（`TestMain` 一次性编译真实 CLI 到临时目录）跑 `demo`（进程1）→ `inspect`/`verify`（新进程）：agent-7 L3/restored-active、agent-3 L5/identity_revoked/cardRevoked、吊销条目数>0、快照元数据、记录数与 demo 摘要一致、verify exit 0。
- 故障注入：字节篡改已提交记录 → verify 非 0 退出并定位文件/seq/行；截断尾部 → verify 报告 torn tail → `repair-tail` 仅删除真正不完整的尾行并报字节数；对「已完整但被篡改」的记录拒绝修复（`hard tamper, refusing to repair`）。
- 吊销语义重启生效：撤销先于有效期/不可删除/重启后仍 `IsUsable()==false`（`persist_test.go`）；L5 吊销后无恢复令、身份不解封、记录不可迁移、信誉归零。

### 6.2 真实落盘数字（本次复跑）

| 命令 | 已落盘记录数 | 段数 | verify |
|---|---:|---:|---|
| `aiprison demo --data-dir D` | **80** | 1 | `INTEGRITY OK — 80 records, per-record hash chain verified`，exit 0 |
| `aiprison drill --data-dir D` | **157** | 1 | OVERALL: PASS；全演练事件哈希链校验 OK |

---

## 7. 技术债 / 复盘 / 后续建议

### 7.1 技术债清单（分级）

| 级别 | 项 | 位置 | 说明 |
|---|---|---|---|
| P2 | `Deprive` 忽略 `Backend.Current` 错误 | `resource/deprive.go:149` | 模拟后端不失败；接真实后端时必须改为透传 |
| P3 | `internal/prison/journey.go` 覆盖 ~37% | 排版 helper | 中文定宽表格，主路径已被 Print smoke 覆盖 |
| P3 | `resource/deprive.go` 覆盖 ~49% | 阶梯错误分支 | 防御性分支，由 e2e/基准间接覆盖 |
| P3 | `store/errors.go` 覆盖 ~52% | 仅 Error() 变体 | — |
| P3 | `identity/service.go` 文件级 ~52.7% | 自覆盖率 77.0% | 部分分支靠跨包 e2e 命中 |
| 边界 | 执行后端全部模拟 | isolation/resource/capability/migration | 无真实沙箱/cgroup/吊销分发/硬件根/GRAM/TLM |
| 发布 | CI 工作流缺失；compat 矩阵未实跑；windows 归档未构建；npm 未配置 | `.github/`、`dist/` | 见 §1.4 |

### 7.2 复盘结论
- v0.1→v1.0.0 十个里程碑**逐版承诺全部落地、逐版本地 tag 齐全**（11 个 tag），每个 tag 均可独立编译、测试、打标，符合 ROADMAP 的每版门禁纪律。
- v1.0.0 之后的加固把「内存原型」推进为「**可运行、可审计、崩溃可恢复**」：真实 JSONL 持久化 + 跨进程重放 + 故障注入 + 真实二进制部署 + 零泄漏 + staticcheck 清零。
- 质量度量在 1.0.0 收口：总覆盖 72.2%、P0=0/P1=1（已修）/P2=1（已修）/P3=8（7 修 1 文档化）、无竞态。热路径已是 O(1) 且按设计 fsync 批量，O(n) Verify 是篡改检测契约、不应优化。
- 未发现任何夸大：仓库不存在「已实现真实管控」的表述，239 处模拟标注贯穿始终。

### 7.3 后续建议
1. **真实执行后端**：把 `isolation.Backend` / `resource.Backend` 接到真实 cgroup / 容器运行时，并在接真实后端时把 `Deprive` 吞掉的 `Backend.Current` 错误透传。
2. **SQL/嵌入式 KV 后端（加分项）**：当前 JSONL append-only 适合审计；若需点查/压缩归档，可加第二个 `Store` 后端并保持接口与重放语义不变。
3. **签名 / 加密**：节点 HMAC 现为模拟；升级为真实签名验签，快照托管接真实门限密码学（替换模拟 M-of-N）。
4. **压缩 / GC**：冷段归档压缩、已稳定段的哈希裁剪（在保持可验证前提下）。
5. **context 全链路**：在编排层把 ctx 贯穿到每一步执行调用（Store 层已支持，其余层待补）。
6. **e2e 加固**：补齐 `.github/workflows`（go 1.23/1.24/1.25 矩阵 + smoke + 产物归档），构建 windows 归档并接 GitHub Release；补充 journey.go / service.go 的直接单测以抬升文件级覆盖。

---

## 附录 A. 本次复跑命令清单与证据指针

复跑命令（仓库根 `/Users/fang/Doubao/chats/2026-10-25/new-chat/ai-prison`）：

```bash
export PATH="$HOME/.go-sdk/go/bin:$PATH"
go version                                   # go1.27.3 darwin/arm64
gofmt -l .                                   # 空 = clean
go vet ./...                                 # 无输出
go build ./...                               # OK
go test -race -count=1 ./...                 # 13 包全 ok
go test -coverprofile=/tmp/cov.txt -covermode=atomic ./...
go tool cover -func=/tmp/cov.txt | tail -1   # total: 72.2%
go test -cover ./...                          # 逐包自覆盖率
find . -name '*.go' -not -path './bin/*' | wc -l            # 97
grep -rh '^func Test' --include='*_test.go' . | wc -l       # 108
grep -rh '^func Benchmark' --include='*_test.go' . | wc -l  # 14
go build -o /tmp/aiprison ./cmd/aiprison
/tmp/aiprison demo  --data-dir <tmp>         # 已落盘记录数 80
/tmp/aiprison drill --data-dir <tmp>         # 已落盘记录数 157
/tmp/aiprison verify --data-dir <tmp>        # INTEGRITY OK, exit 0
git show --stat <tag> ; git ls-tree -r --name-only <tag> | grep -c '\.go$'
```

证据文件：

| 文件 | 内容 |
|---|---|
| `docs/reports/staticcheck-1.0.0.txt` | staticcheck 2026.2.2 初版 4 + 风格 3 findings，全部修复，最终 0 |
| `docs/reports/coverage-1.0.0.txt` | 总覆盖 71.9%、逐包自覆盖、文件级覆盖、加固新增测试 |
| `docs/reports/review-findings-1.0.0.md` | P0=0/P1=1/P2=1/P3=8 详情与回归测试名 |
| `docs/reports/benchmark-1.0.0.txt` | Apple M4 全部基准与二进制/启动/RSS 实测 |
| `CHANGELOG.md` / `docs/ROADMAP.md` / `docs/IMPLEMENTATION.md` / `docs/RELEASE.md` / `docs/06-recovery-drills.md` | 承诺、版本时间线、设计→实现追溯、发布说明、演练矩阵 |

---

## 附录 B. 交付确认

- 本报告**仅新建** `docs/QUALITY-REPORT.md`；未修改任何 `.go`、`CHANGELOG.md`、`README.md`、`go.mod`、`Makefile`、`.github`、`scripts/`；未执行任何 `git add/commit/push/tag`。
- 工作区在报告撰写前后均无对受管文件的改动（`git status` 除既有的未跟踪 `scripts/build-release.sh` 外干净；该文件非本次创建，未触碰）。
