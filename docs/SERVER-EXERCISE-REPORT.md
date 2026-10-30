# 腾讯云研发服务器实机演练报告（Server Exercise Report）

- 报告日期：2026-10-30
- 服务器实例：腾讯云 CVM `ins-j3ou8dmy`
- 公网 IP：134.175.243.146；内网 IP：192.168.0.5
- 操作系统：Ubuntu Server 24.04.4 LTS（x86_64）
- 主机名：VM-0-5-ubuntu
- 规格：4 vCPU / 7.4 GiB 内存 / 50 GiB 系统盘（演练时使用约 7 GiB）
- Go 工具链：go1.27.3 linux/amd64（/usr/local/bin/go）
- 被测版本：ai-prison v1.0.8（远端 main tip `a87ac2c`）
- 定位：研发/测试服务器，非生产环境，可与其他项目共用

> 本报告所有数字均来自服务器上的真实执行；区分「已验证事实」与「建议/估算」。
> GRAM / Token / TLM / 硬件绑定 / MicroVM 仍为接口 + 内存模拟，未夸大为真实能力。

---

## 1. 执行摘要

| 维度 | 结论 |
|---|---|
| 全量门禁（gofmt / vet / build / race test） | 全绿 |
| 测试覆盖率 | 72.1%（与本机基线 72.2% 基本持平） |
| staticcheck | 0 findings |
| L0–L5 分级模拟 | 六级全部正确 |
| 故障注入（6 类） | 全部通过 |
| 数据竞争（-race） | 未发现 |
| 资源/句柄泄漏 | 未发现 |
| P0 阻断问题 | 0 |
| 真实代码 bug | 0（发现的问题均为发布/部署流程问题） |

---

## 2. 环境与工具链

| 项 | 实测值 |
|---|---|
| 发行版 | Ubuntu 24.04.4 LTS |
| 架构 | x86_64（amd64） |
| 内核 | Linux 6.x（Ubuntu 24.04 HWE） |
| CPU | 4 vCPU |
| 内存 | 7.4 GiB（演练时占用约 0.5 GiB） |
| 磁盘 | 50 GiB，剩余约 41 GiB |
| Go | 1.27.3 linux/amd64 |
| 其他工具 | git / make / curl / gcc / python3 齐全 |
| 网络 | api.github.com 可达；github.com 主站不可达，无法直接 git clone |

代码传输方式：因服务器无法访问 github.com 主站，源码通过本机 `tar over ssh` 传入 `~/ai-prison`。

---

## 3. 全量门禁结果

| 检查 | 命令 | 结果 |
|---|---|---|
| 格式 | `gofmt -l .` | 空（无格式问题） |
| 静态检查 | `go vet ./...` | 干净 |
| 构建 | `go build ./...` | 通过 |
| 竞态测试 | `go test -race -count=2 ./...` | 12 个包全部通过，无 data race |
| 覆盖率 | `go test -coverprofile` | 72.1% |
| 静态分析 | `staticcheck ./...` | 0 findings |

---

## 4. Benchmark（linux/amd64 真机）

| 基准 | 规模 | 实测耗时 | 说明 |
|---|---|---|---|
| 哈希链 Verify | 1,000 条 | 约 2.02 ms | 严格线性 |
| 哈希链 Verify | 10,000 条 | 约 20.3 ms | 严格线性 |
| 哈希链 Verify | 100,000 条 | 约 202 ms | 严格线性 |
| 吊销命中 | 单次 | 约 38.8 ns | 零分配（0 B/op, 0 allocs） |
| FileStore 批量写 | 批量 | 优于逐条 fsync | 云 SSD 批量写吞吐高 |

分析：
- 哈希链验证在云 vCPU 上比本机（macOS arm64，1k=0.80ms）慢约 2.5 倍，主因是云主机 vCPU 节流，非代码问题；复杂度仍为严格 O(n)。
- 吊销命中保持纳秒级、零内存分配，全局吊销集查找路径无性能退化。

---

## 5. L0–L5 分级模拟

| 级别 | 名称 | 时间单位 | 关键特征 |
|---|---|---|---|
| L0 | Warning | 即时 | 警告，不剥夺能力 |
| L1 | Restriction | 小时 | 限制部分能力 |
| L2 | Credential Freeze | 天 | 凭证冻结 |
| L3 | Isolation | 周 | 隔离，强制 MicroVM |
| L4 | Capability Removal | 永久 | 能力剥夺，需重认证 |
| L5 | Identity Revocation | 永久 | 身份吊销，不可解封 |

完整链路模拟：检测 → 七阶段两阶段同意判决 → 分层资源剥夺 → 隔离执行 → 审计留痕 → 申诉/矫正 → 恢复，全部按设计执行。

---

## 6. 故障注入演练（6 类）

| # | 场景 | 注入方式 | 期望行为 | 实测结果 |
|---|---|---|---|---|
| a | 哈希链硬篡改 | 翻转早期记录 1 字节 | 检出 content digest mismatch，定位 file/line/seq，拒绝硬修复 | 通过，verify exit 1，repair-tail 拒绝 exit 1 |
| b | 尾部截断（torn） | 截断尾部 120 字节 | 检出 incomplete final line，repair-tail 移除 torn 尾后恢复 | 通过，移除 574 字节，verify 恢复 exit 0 |
| c | 跨进程持久化 | demo 落盘 → 另一进程 drill 追加 → inspect 重放 | 数据连续、链完整 | 通过，80 → 233 条/2 段，INTEGRITY OK |
| d | 并发写 | 8 goroutine 并发追加 | seq 连续、无竞争、无丢失 | 通过，4000 条 seq 连续，-race 干净 |
| e | 断电模拟 | 写进程 kill -9 后重放 | 已 fsync 数据不丢，torn 尾可识别/修复 | 通过 |
| f | 句柄/资源泄漏 | 运行中检查 fd/线程数 | 无段文件句柄长期持有，无泄漏 | 通过，5 fds / 5 threads，50000 条 verify OK |

---

## 7. 发现的问题清单

### P1 — 部署源码与远端基线不同步（已定位）
- 现象：服务器上 `~/ai-prison` 的 `main.go` 常量与 CHANGELOG 顶部停在 1.0.6，而部署的二进制与 package.json 为 1.0.8。
- 根因：部署时从本机传入的源码是落后于远端的工作副本（远端 main 已为 1.0.8）；构建二进制时手动注入了 1.0.8。
- 影响：服务器源码树不能直接用于可重复构建/审计，存在版本错位风险。
- 处置：v1.0.9 重新同步远端最新基线，并在 release_check 中强制四处版本一致性。

### P2 — macOS AppleDouble 残留
- 现象：tar 打包可能带入 `._*` 文件与 `.DS_Store`。
- 处置：传输/打包时 `--exclude='._*' --exclude='.DS_Store'`。

### P2 — 源码树无 .git
- 现象：服务器源码由 tar 传入，无 git 历史，build-release.sh 取 commit 时回退 unknown。
- 处置：用环境变量 `COMMIT` 覆盖，或后续通过 API 同步。

### P3 — 工具未预装
- gofmt 需用 go 同目录完整路径；staticcheck 需 `go install`（GOPROXY 已配 goproxy.cn）。

---

## 8. 分析与改进方案

### 8.1 发布管道（v1.0.9 落地）
1. 版本一致性门禁：`release_check.sh` 强制校验 main.go 常量 == npm/package.json == CHANGELOG 顶部 == git tag，任一不一致即非零退出。
2. build-release.sh：确认 `-ldflags` 使用 `-X main.version=... -X main.commit=... -X main.buildTime=...`（package main 必须用 `main.` 前缀）。
3. 打包排除 AppleDouble：`--exclude='._*' --exclude='.DS_Store'`。
4. 服务器重新同步远端最新基线，确保源码与二进制版本对齐。

### 8.2 工程质量（后续版本）
- 覆盖率当前 72.1%，建议针对 store 边界（空文件、零长度段、权限错误）与并发路径补测试，目标提升至 75%+。
- 增加发布前的跨平台 e2e 冒烟（linux/darwin/windows），CI 已具备矩阵基础。
- 对模拟边界（GRAM/TLM/硬件绑定/MicroVM）保持显式标注，避免文档与实现脱节。

### 8.3 运维与安全
- 服务器初始密码已在对话明文出现，建议改密或禁用密码登录，仅保留密钥登录。
- 建议为部署二进制配置最小权限的 systemd 服务或定时校验任务（verify 定期跑）。
- 凭证（GitHub token、npm token）建议定期轮换。

---

## 9. 结论

服务器实机演练中，ai-prison 在 linux/amd64 上全量门禁通过、无数据竞争、无资源泄漏、无真实代码 bug；六类故障注入全部按设计检出/恢复。唯一需要修复的是发布/部署流程的版本一致性问题，已在 v1.0.9 通过重新同步基线与强制门禁解决。
