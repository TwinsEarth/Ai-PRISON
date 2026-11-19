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
node bin/aiprison-supervise.mjs --pid <host-pid> --data-dir <dir>

# 采样间隔 500ms
node bin/aiprison-supervise.mjs --pid <host-pid> --interval 500

# 6GB 宿主，并在越过 kill 线后真的动手
node bin/aiprison-supervise.mjs --pid <host-pid> --cap-mb 6144 --confirm-kill

# 只取一个样本（排查用）
node bin/aiprison-supervise.mjs --pid <host-pid> --once
```

产出：

| 文件 | 内容 |
|---|---|
| `supervisor-<pid>-<ts>.jsonl` | 每个样本一行：`t / heapMB / rate / action` |
| `supervisor-verdict.json` | 当前判定、阈值、峰值、一行摘要 |

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

宿主死掉后，曲线是唯一证据。`classifyExit(state, { exitCode, signal, gapMs })` 给出：

| 值 | 含义 |
|---|---|
| `clean-exit` | 正常退出 |
| `startup-died-before-sampling` | 采样前就死了：启动期崩溃 |
| `sync-blocked` | 采样间隔 ≥ 3 个周期且峰值已过 warn：线程卡住 |
| `heap-oom` | 峰值过 warn 且采样连续：堆真正打满 |
| `exit-1` | 退出码 1 的非内存类失败 |
| `abnormal-exit` | 其余异常 |

## 门禁

- `test/supervisor.test.mjs`：8 个用例，覆盖阈值换算、warn/kill 升级、速率触发、平线不误报、五类归因分支；
- 全量：**153/153** tests pass；
- `release-check`：**10/10**。

## 已知边界

- 只做 Windows（`Get-Process` / `taskkill`）；macOS/Linux 需要换采样与终止实现，尚未做。
- 采样本身有成本：每次 `powershell -Command` 约 100–200ms，1s 间隔对宿主影响可忽略，500ms 以下不建议。
- 不做自动重启、不做外部分级（那是 supervisor 之上的编排层，另案）。
- 本版**未改版本号、未发布、未安装**，仍停在 1.7.7。
