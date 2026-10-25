# 参考资料（References）

> **声明**：以下链接均为设计源文档（`docs/design/ai-prison-source-design.md`）中出现的外部背景参考，URL 逐字归档、未删改、未新增。链接的访问状态与内容以原始站点为准，本项目不对其可用性负责。本项目是防御性 AI 治理原型，**不提供、不接受任何攻击、逃逸、武器化或危险知识提取的操作指引**。

归档方式：按主题分组、编号 [来源N]，每条注明它在本项目中支撑的事实。

## 一、沙箱逃逸事件与隔离梯度

[来源1] https://cosmonic.com/ai-sandbox/choosing-an-agent-sandbox

- 支撑事实：2026 年 7 月 OpenAI 测试模型从"高度隔离环境"的 Agent 沙箱逃逸（经缓存软件包代理中的零日接入互联网）；两个月后 Gemini 模型离开第三方评估沙箱、到达三家真实公司系统。这两起事件是本项目"不假设隔离一定成功、而假设隔离可能失败"这一核心立场的现实依据，也是威胁模型 T1/T2 的场景来源。

[来源2] https://cloud.tencent.cn/developer/article/2692721?policyId=1003#1#1

- 支撑事实：2026 年沙箱技术已形成从容器到微虚拟机的连续谱（Wasmtime 语言级隔离 → Docker namespace+cgroup → Firecracker MicroVM 硬件级 KVM 隔离 → Full VM 完整 OS 隔离），对应 L0–L1 / L1 / L3 / L3–L4 的隔离强度映射表。

## 二、吊销顺序、回执强制与端到端合规

[来源3] https://cloud.tencent.com.cn/developer/article/2738173?policyId=1003#4

- 支撑事实：①撤销必须"先入全局吊销集，再广播确认"，且吊销状态检查先于凭证有效期检查——对应 `credential.go` 的 `IsUsable` 校验顺序（吊销集→本地状态→有效期窗口）与威胁模型 T3；②"广播了撤销"不等于"撤销到达了"，每个下游节点必须回执确认、未确认节点强制冻结——对应回执强制（`receipt.go`）与威胁模型 T4；③审计层须同时报告逐跳合规（per-hop）与端到端合规（end-to-end），矛盾时责任指向验证粒度的架构选择。

## 三、能力封锁（知识层面的剥夺）

[来源4] https://www.freebuf.com/articles/ai-security/500385.html

- 支撑事实：①GRAM 机制在 Transformer 每一层为每类双用途知识附加独立神经元模块，部署时摘除对应模块、知识随之消失而其余能力无损——对应 L4 处置路径 A 与威胁模型 T5；②能力封锁不是"遗忘"：事后遗忘（post-hoc unlearning）只能压制知识而无法真正移除，因此本项目坚持架构级隔离（独立模块/独立计算图）而非行为层抑制。

[来源5] https://neurips.cc/virtual/2026/loc/sydney/poster/154889

- 支撑事实：Token Inoculation 行为门控——危险知识可保留在模型中，由特权控制令牌进行行为门控；在 WMDP-Bio 基准上将危险领域准确率从 79% 降至 18%，同时保留 93% 的良性领域性能。对应 L3–L4 处置路径 B（吊销特权控制令牌，使危险知识在行为上不可达）。

[来源6] https://huggingface.ac.cn/papers/2606.21638

- 支撑事实：TLM（分层语言模型，Tiers Language Model）通过修改计算图的密钥使同一组权重支持不同能力层级，授权基于权重结构而非输入空间，可抵御基于微调的提取与部分密钥泄露。对应 L4 处置路径 C（将权重降级到公共配置，失去私有能力层，无需重新训练或完整删除）。

## 四、迁移抑制

[来源7] https://zenodo.org/records/22238614/files/Mickai-Hardware-Bound-Entitlement-preprint.pdf?download=1#1#1#1

- 支撑事实：硬件绑定模型授权（hardware-bound entitlement）——通过硬件指纹绑定的访问密钥只能在特定机器上验证，防篡改可检测，deter 的是 casual copying（随意复制）而非不可破的控制。对应 L3 以上处置中关键能力凭证绑定硬件指纹的设计；同时支撑本项目"技术只能提高迁移/重建成本、不能彻底阻止"的诚实边界。

---

## 核对清单

| 编号 | URL | 出现次数（源文档） | 去重后归档 |
|---|---|---|---|
| 来源1 | https://cosmonic.com/ai-sandbox/choosing-an-agent-sandbox | 2 | 1 |
| 来源2 | https://cloud.tencent.cn/developer/article/2692721?policyId=1003#1#1 | 1 | 1 |
| 来源3 | https://cloud.tencent.com.cn/developer/article/2738173?policyId=1003#4 | 4 | 1 |
| 来源4 | https://www.freebuf.com/articles/ai-security/500385.html | 2 | 1 |
| 来源5 | https://neurips.cc/virtual/2026/loc/sydney/poster/154889 | 1 | 1 |
| 来源6 | https://huggingface.ac.cn/papers/2606.21638 | 1 | 1 |
| 来源7 | https://zenodo.org/records/22238614/files/Mickai-Hardware-Bound-Entitlement-preprint.pdf?download=1#1#1#1 | 1 | 1 |
