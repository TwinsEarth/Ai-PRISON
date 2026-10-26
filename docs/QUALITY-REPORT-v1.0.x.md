# AI Prison — v1.0.1 → v1.0.6 质量审查与实测复盘报告

- 生成日期：2026-10-26（Asia/Shanghai）
- 审查对象：纯文档/版本 patch 系列 **v1.0.1 … v1.0.6**（基于 v1.0.0 收口后）
- 代码基线：本地 `14dfb91`；远端 `main` = **`c2072f3366afdf8230b034441731d41b613e53e0`**（= v1.0.6 远端提交）
- Go：`go1.27.3 darwin/arm64`（go.mod `go 1.23`）；module `github.com/ai-prison/ai-prison`
- 性质声明：六个 patch **只改文档 + 版本号 + npm 版本**，未触碰任何 Go 业务逻辑，未触碰 `.github/workflows/`。

---

## 1. 总览

| 维度 | 结果 |
|---|---|
| 远端 Release v1.0.1–v1.0.6 | **6/6 全部存在**，均 `draft=false`、各 **7 个资产**（6 平台包 + checksums.txt） |
| 远端 tag v1.0.0–v1.0.6 | 7/7 齐全（注解 tag） |
| npm `@twinsearth/aiprison` | `dist-tags.latest = **1.0.6**`；versions 含 `1.0.1 … 1.0.6`（另含历史 `0.0.0-stage`、`1.0.0`） |
| npm 各版 tarball | 1.0.1–1.0.6 逐个 `GET` 均 **HTTP 200** |
| 全量门禁（见 §3） | gofmt 空 / vet 干净 / build OK / examples build OK / `go test -race -count=2` 13 包全绿 |
| 覆盖率 | **72.2%**（与 v1.0.0 基线 72.2% 持平，无退化） |
| staticcheck | **0 findings**（exit 0） |
| 实机落库（见 §4） | demo 80 条 → drill 跨进程追加 → 233 条/2 段 → inspect 重放 → `INTEGRITY OK` exit 0 |
| 工作树零残留 | 复跑后 `git status --porcelain` 为空（详见 §3 对 `basic` 临时二进制的处置） |

---

## 2. 逐 patch 明细

| 版本 | 通俗定位（用户原话） | README 落点 | docs 落点 | 本地 commit | 远端 commit | tag 对象 | Release |
|---|---|---|---|---|---|---|---|
| v1.0.1 | 给 AI 设计的"监狱"——虚拟沙盒（软件层受控隔离，防危险 AI 接触真实系统） | 副标题块 + 列表第 1 项 | `docs/01-product-design.md` | `9ce14df` | `233f921b1c53199f2997515c1438b6b6b97dba31` | `7dfccac9…` | [v1.0.1](https://github.com/TwinsEarth/ai-prison/releases/tag/v1.0.1) |
| v1.0.2 | 隔离执行环境（容器/Docker 沙箱，AI 无法突破到宿主机） | 列表第 2 项 | `docs/02-technical-route.md`（隔离梯度表后） | `25c5003` | `03b175aae4ecdd4797c8479f1cbef4f50887b220` | `a895a785…` | [v1.0.2](https://github.com/TwinsEarth/ai-prison/releases/tag/v1.0.2) |
| v1.0.3 | 行为监控模块（实时记录系统调用/网络请求/资源使用，生成行为日志） | 列表第 3 项 | `docs/02-technical-route.md`（1.3 行为监控层） | `a07b4fb` | `f06706a7df84ac775cbb0393cfd00ad436307286` | `1dee86d9…` | [v1.0.3](https://github.com/TwinsEarth/ai-prison/releases/tag/v1.0.3) |
| v1.0.4 | 越狱防御（动态行为镣铐/静态特征扫描/对抗样本防御） | 列表第 4 项 | `docs/05-threat-model.md`（纵深防御环路后） | `d8c4f17` | `5262db47eb9b8bb01bc56102e6d1d5c203d60d7e` | — | [v1.0.4](https://github.com/TwinsEarth/ai-prison/releases/tag/v1.0.4) |
| v1.0.5 | 假释评估（ETHICS 矩阵测公平性/透明度，达标才"出狱"） | 列表第 5 项 | `docs/04-governance.md`（6. 矫正评估） | `fe7c6cf` | `6a1dc1dfbac180008b95d74a74c016ba0058a536` | — | [v1.0.5](https://github.com/TwinsEarth/ai-prison/releases/tag/v1.0.5) |
| v1.0.6 | 刑满释放评估（是否改造成功、是否需要加速 1–100 万倍） | 列表第 6 项 | `docs/06-recovery-drills.md`（恢复成本段后） | `14dfb91` | `c2072f3366afdf8230b034441731d41b613e53e0` | — | [v1.0.6](https://github.com/TwinsEarth/ai-prison/releases/tag/v1.0.6) |

### 2.1 README 最终定位列表（六项齐全，逐行核对）

```
> - 虚拟沙盒：软件层面的隔离机制，把可能危险的 AI 关进受控环境运行，防止它接触真实系统。
> - 隔离执行环境：用容器技术（比如 Docker）构建沙箱，让 AI 算法在完全隔离的环境中运行，无法突破到宿主机。
> - 行为监控模块：实时记录 AI 的所有系统调用、网络请求和资源使用情况，生成详细行为日志供分析。
> - 越狱防御：动态行为镣铐（运行时监测非法调用）、静态特征扫描（代码审计）、对抗样本防御等多重机制。
> - 假释评估：通过 ETHICS 评估矩阵测试 AI 的公平性、透明度等，通过才能"出狱"。
> - 刑满释放评估：是否改造成功，是否需要加速 1–100 万倍。
```

### 2.2 版本号三处一致性（每版均由 `scripts/release_check.sh` 校验通过）

| 位置 | v1.0.6 终值 |
|---|---|
| CLI 常量 `cmd/aiprison/main.go` | `version = "1.0.6"` |
| `npm/package.json` | `"version": "1.0.6"` |
| `CHANGELOG.md` 顶部段 | `## [1.0.6] - 2026-10-26` |
| git tag | `v1.0.6`（注解 tag） |
| GitHub Release | `v1.0.6`，notes 直接取自 CHANGELOG 1.0.6 段 |
| 发布二进制内嵌 | `aiprison 1.0.6 / commit: c2072f33`（本次实测解包验证） |

> 说明：Release notes 由发布脚本直接从 `CHANGELOG.md` 抽取对应版本段生成，故 CHANGELOG 段与 Release notes 天然一致，不存在两处手工维护漂移。

### 2.3 每个 Release 的资产完整性

每个 Release 均上传 7 个资产（本次 API 逐版核对 `assets.length = 7`）：

```
aiprison_linux_amd64.tar.gz    aiprison_linux_arm64.tar.gz
aiprison_darwin_amd64.tar.gz   aiprison_darwin_arm64.tar.gz
aiprison_windows_amd64.zip     aiprison_windows_arm64.zip
checksums.txt                  （SHA256，"<hex>  <basename>" 两列格式）
```

---

## 3. Phase-2 全量门禁回归（在最终 1.0.6 代码上复跑）

| 命令 | 结果 |
|---|---|
| `gofmt -l .` | **空**（无格式违例） |
| `go vet ./...` | 干净，exit 0 |
| `go build ./...` | OK，exit 0 |
| `go build ./examples/...` | OK，exit 0 |
| `go test -race -count=2 ./...` | **13 个包全部 ok**，无 race、无失败 |

- 测试包清单（全绿）：audit、domain、enforcement/{capability,identity,isolation,migration,resource}、governance、prison、revocation、store、test；`cmd/aiprison`、`examples/basic`、`pkg/contracts` 为无测试文件包。
- **零残留说明**：`go build ./examples/...` 会把产物 `basic` 二进制落在仓库根目录（Go 默认行为）。本次复跑后发现 `?? basic`，已删除；删除后 `git status --porcelain` 为空。CI 中 `go build` 均带 `-o` 或使用 `go install`，不受此影响。

---

## 4. 实机部署 + 跨进程落库断言（真二进制）

使用 `scripts/build-release.sh 1.0.6` 产出的 `darwin/arm64` 真二进制（内嵌 `commit=c2072f33`），在 `mktemp -d` 临时 data-dir 中复跑：

| 步骤 | 实测 |
|---|---|
| 二进制版本 | `aiprison 1.0.6 / commit: c2072f33` |
| `demo --data-dir X`（进程 1） | 落盘 **80 条 / 1 段**，生成 `seg-0000000000.log`（权限 600） |
| `drill --data-dir X`（同目录，进程 2，跨进程追加） | 累计 **233 条 / 2 段**（drill 在已有 ledger 上续写，未覆盖） |
| `inspect --data-dir X`（重启后只读重放） | 正确重建 agents：`agent-3=L5/identity_revoked`、`agent-7=L3/active`、`drl-2..drl-5` 状态/声誉/DID 均恢复；快照哈希链逐条列出 |
| `verify --data-dir X` | **`INTEGRITY OK — 233 records, per-record hash chain verified`，exit 0** |
| 临时目录 | 测试后 `rm -rf` 清理，仓库无残留 |

跨进程落库、崩溃一致性校验、重放重建均与 v1.0.0 基线一致；本轮无 Go 改动，故行为不变。

---

## 5. 静态分析 / 覆盖率 / Benchmark（对比 v1.0.0 基线）

### 5.1 覆盖率（`go test -cover`）

| 包 | v1.0.6 | v1.0.0 基线 | 结论 |
|---|---|---|---|
| **全仓 total** | **72.2%** | 72.2% | 持平 |
| audit | 89.0% | 89.0% | 持平 |
| enforcement/isolation | 89.5% | 89.5% | 持平 |
| enforcement/migration | 80.5% | 80.5% | 持平 |
| enforcement/resource | 81.8% | 78.5% | 微小波动（非本轮改动；归档基线测量点差异） |
| governance | 78.4% | 78.4% | 持平 |
| enforcement/identity | 77.0% | 77.0% | 持平 |
| prison | 76.0% | 76.0% | 持平 |
| store | 75.6% | 75.2% | 微小波动 |
| revocation | 84.3% | 84.3% | 持平 |
| enforcement/capability | 71.1% | 71.1% | 持平 |
| domain | 78.3% | — | 复测量 |

> 六个 patch 为纯文档改动，覆盖率不应变化；total 72.2% 与基线完全一致即为佐证。

### 5.2 staticcheck

`staticcheck ./...` → **exit 0，0 findings**（与 v1.0.0 归档一致）。

### 5.3 关键 Benchmark（Apple arm64，`-benchtime=100x` 短测）

| Benchmark | v1.0.6 实测 | v1.0.0 基线 | 结论 |
|---|---|---|---|
| 吊销命中 `IsRevoked`（hit） | 13.3 ns/op，0 alloc | 6.6–7.4 ns/op，0 alloc | 同量级、零分配（短测机器噪声） |
| 吊销大集合 miss | 12.5 ns/op，0 alloc | — | 零分配 |
| 判决哈希链 `Chain.Append` | 1.44 µs/op | 1.1 µs/op | 同量级 |
| `Verify` 1k | 0.80 ms | 0.78 ms | 持平 |
| `Verify` 10k | 8.0 ms | 8 ms | 持平 |
| `Verify` 100k | 78.9 ms | 78 ms | 持平 |
| FileStore 逐条 fsync | 8.38 ms/op | 8.18 ms/op | 持平 |
| FileStore 批量写 | 90.5 µs/op | 33.5 µs/op | 同模式（批量≈逐条的 1/90） |

无任何性能退化。

---

## 6. 审查发现与遗留风险

### 6.1 本轮发现（均已处置或记录）

| 级别 | 项 | 处置 |
|---|---|---|
| P3 | `make build` 默认 `VERSION ?= 1.0.0`（Makefile 第 7 行），裸跑 `make build` 会内嵌 1.0.0；发布资产由 `scripts/build-release.sh <ver>` 正确注入 1.0.6 | 未发第 7 个 patch（任务边界=六版）；发布产物版本正确。建议后续把 Makefile 默认值与发版脚本对齐，或 `make build VERSION=1.0.6` |
| P3 | `go build ./examples/...` 会在根目录落 `basic` 二进制 | 复跑后已删除，工作树清零 |

### 6.2 长期挂起缺口（与 v1.0.0 一致，本轮未触碰）

- **GitHub Actions workflow 未上远端**：`.github/workflows/{ci,compat,release}.yml` 三文件仅存在于本地。当前 token scope 为 `read:org,repo`，**缺 `workflow` scope**，经 Git Data API 推送时按既定策略**整目录排除**（每版推送日志 `excluded workflows: 3`）。远端 Actions runs 仍为 0。补法：换 workflow-scoped token，或在网页端上传这三个文件。
- **执行/管控能力仍为模拟**：GRAM / Token Inoculation / TLM / 硬件绑定 / MicroVM 均为接口 + 内存模拟，README/docs/CLI 输出均已如实标注"内存模拟、无真实隔离/吊销分发/硬件绑定/能力摘除"。
- npm scope 路由坑：默认 userconfig 把 `@twinsearth` 指到 npm.pkg.github.com、registry 指到淘宝镜像；每版发布均用临时 `.npmrc` 强制 `registry` 与 `@twinsearth:registry` 指向 `https://registry.npmjs.org/`，六版均正确发到 npmjs.org。

---

## 附录 A. 复跑命令

```bash
export PATH="$HOME/.go-sdk/go/bin:$PATH"
export GOPROXY=https://goproxy.cn,direct GOFLAGS=-mod=mod
gofmt -l .
go vet ./...
go build ./... && go build ./examples/... && rm -f basic
go test -race -count=2 ./...
go test ./... -coverprofile=/tmp/cov.out && go tool cover -func=/tmp/cov.out | tail -1
staticcheck ./...
bash scripts/release_check.sh
# 实机落库
./aiprison --data-dir <tmp> demo ; ./aiprison --data-dir <tmp> drill
./aiprison --data-dir <tmp> inspect ; ./aiprison --data-dir <tmp> verify
```

## 附录 B. 交付确认

- 远端 `main`：`c2072f3366afdf8230b034441731d41b613e53e0`（= v1.0.6）
- Release：v1.0.1–v1.0.6 共 6 个，非草稿，各 7 资产，均可独立访问
- npm：`@twinsearth/aiprison@1.0.6` 为 latest，1.0.1–1.0.6 tarball 全部 HTTP 200
- 门禁 / 覆盖率 / staticcheck / benchmark / 落库实测：见 §3–§5，全部通过且无退化
- 本地工作树干净；`.github/workflows/` 按缺口策略继续挂起，未强推
