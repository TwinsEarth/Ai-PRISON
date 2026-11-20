# 外部内存 supervisor（1.8.0 · 隐藏功能）

覆盖进程内 watchdog 的两处结构性盲区：

1. **同步阻塞**：宿主 JS 线程被长任务独占时，watchdog 的采样定时器不跳，内存涨到 OOM 也没人看见。
2. **启动期**：插件加载前后、baseline 建立之前的暴涨（历史上 5.4 GB / 4.03 GB 的两次 OOM 都在这个窗口）。

设计约束来自 `AI-PRISON-HARDENING-1.6.0.md` §8，本实现未突破任何一条：

- 不替换 `app.asar`、不改安装目录、不打宿主补丁；
- 唯一形态是独立进程，用操作系统 API 读宿主 RSS；
- 不写客户端 profile、不需要管理员安装、不依赖插件行；
- 默认隐藏：面板无按钮、工具列表无入口、无配置项；
- 默认只观察，强制结束必须显式加 `--confirm-kill`。

## 用法

```bash
# 只观察：每分钟建一条曲线，写 verdict，不动进程
node scripts/aiprison-supervise.mjs --pid <host-pid> --data-dir <dir>

# 采样间隔 500ms
node scripts/aiprison-supervise.mjs --pid <host-pid> --interval 500

# 6GB 宿主，并在越过 kill 线后真的动手
node scripts/aiprison-supervise.mjs --pid <host-pid> --cap-mb 6144 --confirm-kill

# 只取一个样本（排查用）
node scripts/aiprison-supervise.mjs --pid <host-pid> --once
```

产出：

| 文件 | 内容 |
|---|---|
| `supervisor-<pid>-<ts>.jsonl` | 每个样本一行：`t / rssMB / rate / cpuRatio / action` |
| `supervisor-verdict.json` | 当前判定、阈值、峰值、CPU 峰值、`gapMs`、一行摘要 |

`cpuRatio` 是本周期内目标进程消耗的 CPU 秒数 ÷ 墙钟秒数（1.0 = 占满一个核），
与 RSS 同一次 `Get-Process` 查询取回，不增加采样开销。

## 判定规则

阈值按宿主 heap cap 等比换算，与进程内水位线同源：

| 档位 | 4 GB 宿主 | 6 GB 宿主 |
|---|---|---|
| warn（73%） | 2990 MB | 4485 MB |
| kill（93%） | 3809 MB | 5714 MB |

- 越过 warn：只记录 + `warn` 判定，不动作；
- 越过 kill **连续 3 次**（默认 1s 间隔，即约 3s）：发一次 `request-exit`，宽限 `graceMs`（默认 5s）后仍未退出 → `kill`；
- 增长速度 **≥ 150 MB/s 连续 2 次**：不等绝对阈值，直接 `request-exit`（这就是启动期和同步阻塞场景的兜底）；
- `request-exit` 只发一次，不会刷屏。

## 事后归因

宿主死掉后，曲线是唯一证据。`classifyExit` 由 CLI 在"读不到进程"时真正调用（此前只在单测里存在）。判据全部基于外部可观测证据：

| 值 | 判据 |
|---|---|
| `clean-exit` | 调用方明确给了退出码 0 |
| `startup-died-before-sampling` | 一个样本都没采到：启动期崩溃 |
| `sync-blocked` | ①采样间隔 ≥ 3 个周期且峰值过 warn（采样器自身被饿死，罕见）；或 ②**峰值在 warn 与 kill 之间、且 CPU 占用连续 ≥ 0.8** —— 内存没有继续撞向 kill 线，是线程卡住烧 CPU |
| `heap-oom` | 峰值过 warn（含越过 kill 线的情况）：栈真的往上涨 |
| `exit-requested` | 加了 `--confirm-kill` 且已发出退出请求，进程随后退出：因我们的动作而死，不报成崩溃 |
| `supervisor-kill` | 宽限期后仍存活、由 supervisor 强杀（该路径直接写 verdict，不参与推断） |
| `exit-1` | 退出码 1 的非内存类失败 |
| `abnormal-exit` | 其余异常（外部无法拿到退出码时也落在这里） |

> 为什么不用"采样间隔 ≥ 3 个周期"当主判据：进程一消失，下一个 tick 就会发现并结束采样，`gapMs` 只可能约等于一个间隔。本机实测 493–539ms（间隔 500ms），永远达不到 1500ms，该分支在真实退出路径上不可达。

## 真机验收（Windows，2026-11-21）

受控替身是一个独立 node 进程（不接触 DSH 客户端），行为固定：延迟 N 秒 → 分配并逐页触碰目标内存 → 同步自旋 2 秒（事件循环完全占住）→ 以退出码 3 结束。supervisor 用 `--cap-mb 512`（warn 374 / kill 476）、500ms 间隔。

| 用例 | 替身行为 | 采样结果 | 归因 |
|---|---|---|---|
| A 同步阻塞 | 420 MB，峰值 456 MB（warn 与 kill 之间），CPU 0.71→0.98 | 8 样本，500ms 节奏不断 | **`sync-blocked`** |
| B 越过 kill 线 | 620 MB，峰值 656 MB，CPU 0.92 | 3 次连击 → `request-exit`（仅观察） | **`heap-oom`** |
| C 请求后退出 | 620 MB，`--confirm-kill` | `request-exit` 后进程立即消失 | **`exit-requested`** |

A 的价值在于：**宿主线程被完全卡住的 2 秒里，外部 supervisor 的采样一次都没停**，这正是进程内 watchdog 做不到的那一段。B/C 证明 CPU 证据不会把真涨内存误判成阻塞，也不会把"我们请它退出"包装成崩溃。

证据目录：`E:\DB\AiPrison\evidence\ai-prison-1.8.0-supervisor-real-20261121\`（`case-A-sync-blocked` / `case-B-heap-oom` / `case-C-exit-requested`，各含 `supervisor.out.txt`、`supervisor-verdict.json`、曲线、替身日志）。

## 门禁

- `test/supervisor.test.mjs`：**12 个用例**，覆盖阈值换算、warn/kill 升级、速率触发、平线不误报、归因七分支（含 CPU 饱和判据、强杀、请求后退出）；
- 全量：**157/157** tests pass；
- `release-check`：**11/11**。

## 已知边界

- 只做 Windows（`Get-Process` / `taskkill`）；macOS/Linux 需要换采样与终止实现，尚未做。
- 采样本身有成本：每次 `powershell -Command` 约 100–200ms，1s 间隔对宿主影响可忽略，500ms 以下不建议。
- 外部进程拿不到宿主的退出码，因此非内存类死亡只能落在 `abnormal-exit`；`clean-exit` 仅当调用方明确传入 0 时才成立。
- `sync-blocked` 的 CPU 判据有一个残余误判面：**峰值恰在 warn 与 kill 之间、又因 GC 抖动把 CPU 打满的真实 OOM** 会被归成阻塞。两类的处置动作相同（都是 `request-exit`），归因文案才有差别，暂不为它引入更重的信号（如句柄/ETW）。
- 不做自动重启、不做外部分级（那是 supervisor 之上的编排层，另案）。
- 本版**未改版本号、未发布、未安装**，仍停在 1.7.7。


