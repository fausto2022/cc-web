# cc-web 设计方案

状态：初版设计，供实现前评审

## 1. 目标与原则

`cc-web` 是面向 Linux 无图形环境的自托管 Web Agent 工作台。服务进程运行在 Linux 主机，浏览器负责交互；项目文件、CLI 进程、凭据和任务均留在主机。首期支持 Codex CLI 与 Claude Code CLI，通过 provider adapter 隔离各自的启动参数、流事件、会话恢复与审批语义。

设计目标：

- 复用 `cc-haha` 的工作台心智模型：左侧项目/会话导航，中间对话与任务状态，右侧文件变更/Diff/终端面板。
- 将所有执行和授权集中在服务端；浏览器不直接访问文件系统或 CLI。
- 断线后任务继续运行，重连后从服务端状态和事件游标恢复。
- 优先实现可靠的会话生命周期、审批、文件审阅和终端体验，再逐步扩展桌面版高级能力。
- 单机/单用户优先。多用户隔离作为后续明确的安全设计，不以“有登录页”假装已具备隔离能力。

## 2. 桌面版现状与 Web 实现范围

检查依据：`cc-haha` README 的桌面端亮点、`docs/desktop/index.md`、`docs/desktop/remote.md`、`docs/internals/desktop.md`、`docs/internals/server.md` 与 `desktop/src` 目录结构。当前产品不是单纯 Electron UI：React renderer 与 Server Sidecar、CLI 子进程、Electron Host 分进程协作；已有 H5 是受限远程客户端，不是完整桌面 Web 部署。H5 运行时会走浏览器 Host，但终端、Provider/MCP/技能管理、嵌入式浏览器和 Computer Use 等桌面 Host 能力明确不可用。Server 和 CLI 所在主机就是代码/命令执行位置，因此远程 Web 部署需要重新设计安全边界。现有 REST 与浏览器 `/ws/:sessionId` 可作为产品/API 参考，但聊天通道最终落到 Claude 专用会话服务；内部 `/sdk/:sessionId` 是 Claude CLI 私有回连协议，不能作为 Codex 接口复用。新项目需在服务端抽出 runtime adapter，而不是只换 CLI executable。

| 桌面版能力 | Web 版首期 | 实现策略/说明 |
|---|---|---|
| 项目切换、会话列表/标签、会话历史、全局搜索 | 支持 | 服务端项目/会话资源；浏览器维护打开标签和布局偏好 |
| 新建会话、模型/运行参数、权限模式 | 支持 | 表单创建；模型/effort/审批选项按 adapter 能力显示 |
| 流式聊天、工具调用、思考/状态、停止、断线恢复 | 支持 | WebSocket 事件协议，服务端保存事件与状态快照 |
| 工具权限/问题/计划审批 | 支持 | 每项审批明确显示来源、参数和影响；服务端校验会话与待审批 ID |
| 工作区文件列表、Diff、逐文件审阅、整轮回退 | 部分支持 | 先做变更清单、统一 Diff 与安全审阅；撤销通过 Git 变更恢复能力，不承诺恢复任意未提交/未跟踪文件 |
| 终端 | 支持（受控 PTY） | cc-haha 当前 H5 明确不开放终端；cc-web 单独实现 xterm.js + 服务端 PTY，并加入设备授权、会话恢复、撤销、鉴权和生命周期清理 |
| Git 分支、Worktree | 部分支持 | 首期读取状态/选择分支；隔离 worktree 作为可选会话工作目录，需验证 CLI 对每个 provider 的支持 |
| 内置浏览器预览 | 替代实现 | 通过受限代理/端口预览；不等同 Electron 的带真实登录态原生浏览器，首期只预览明确授权的本机服务 |
| 五档权限模式 | 部分支持 | 仅呈现各 CLI 有对应语义的选项；统一“请求审批”优先，不虚构跨 CLI 一致语义 |
| MCP 管理、Skills、Agents | 后续 | 先由用户 CLI 配置管理；之后增加只读发现和受控写入，不在首期复制完整市场 |
| 定时任务、用量统计、后台活动 | 后续 | 需建立任务调度、执行记录和各 CLI 用量口径；不阻塞核心工作台 |
| Agent Teams、动态 Workflow | 后续/实验 | 依赖不同 CLI 能力差异，先抽象 capabilities 再逐步接入 |
| Computer Use、桌面宠物、原生窗口/系统托盘 | 不纳入 | Linux headless 无桌面会话/图形 API；不适合以 Web 服务伪装实现 |
| IM 接入、手机接力 | 后续 | Web 响应式界面可先解决移动浏览器；IM 连接器另行部署和授权 |
| Provider/API 管理及 OAuth | 部分支持 | CLI 自己维护的认证优先；Web 管理凭据必须加密存储并做密钥轮换，首版不代理模型 API |

## 3. 系统架构

```text
浏览器 (React + TypeScript)
  ├─ REST：项目、会话、设置、文件审阅、终端生命周期
  └─ WebSocket：流式事件、审批、状态同步、终端输入输出
          │ HTTPS / WSS
          ▼
Go Web 服务（单一部署单元）
  ├─ Auth / CSRF / Origin / RBAC 边界
  ├─ API 与 WebSocket Hub（会话房间、事件游标、重连快照）
  ├─ Session Orchestrator（状态机、停止、恢复、超时与清理）
  ├─ Provider Registry
  │    ├─ Claude Code Adapter → claude CLI 子进程
  │    └─ Codex Adapter       → codex CLI 子进程
  ├─ Workspace Service（根目录白名单、Git、文件读取与 Diff）
  ├─ PTY Service（可选，终端独立进程/伪终端）
  └─ SQLite（用户/项目/会话元数据、事件索引、设置；文件内容仍在工作区）
          │
          ├─ Claude Code CLI（由服务端用户配置/认证）
          ├─ Codex CLI（由服务端用户配置/认证）
          └─ Linux 文件系统 / Git / 用户启动的预览端口
```

建议初始技术选型：Go 作为服务端、React + TypeScript + Vite 前端、SQLite 持久化、WebSocket 实时通道、xterm.js 终端、成熟 PTY 库（选定前核对 Linux 支持与许可证）。发布优先单个 Go 服务提供 API 和静态资源，避免首期引入微服务、Redis 或外部数据库。

### 服务端边界

- 所有项目路径必须先经管理员配置的根目录白名单解析；拒绝路径穿越、符号链接逃逸和未授权绝对路径。
- CLI 以非 root 专用 OS 用户运行，使用精确 argv 和明确工作目录，禁止 shell 拼接命令。
- 每个会话记录 provider、进程 ID/进程组、工作目录、CLI session ID、权限状态和最后事件序号。
- 日志/事件不得记录 API Key、OAuth token、环境变量全量或敏感文件内容。
- 浏览器 API 与 WebSocket 一致鉴权；WebSocket 建连后仍校验 session/project 所属关系。
- 默认只绑定 `127.0.0.1`。远程使用须配置 HTTPS 反向代理、强认证、可信 Origin、CSRF 防护和独立访问令牌；不把 CORS 当认证。

### Web 界面布局

沿用桌面版三块空间，但适配浏览器：

- 左栏：品牌/新建会话、项目、会话历史、搜索；宽度可调，窄屏折叠。
- 顶栏：会话标签、运行状态、停止、活动/终端/工作区切换。
- 中区：消息时间线、工具调用卡、审批卡、Markdown/代码/错误状态及输入框。
- 右侧工作区：变更文件清单、Diff、Git 状态；窄屏使用抽屉视图。
- 输入工具栏：provider、模型/effort（仅能力支持时显示）、权限策略、附件（后续）、发送/停止。
- 视觉上延续 cc-haha 的克制工作台布局，不复制 Electron 专属窗口行为；深浅色及配色主题列为后续。

## 4. CLI 适配策略

Codex CLI 与 Claude Code CLI 不是同一个底层协议，不能假定它们都兼容 cc-haha 内部 Claude SDK，也不能仅通过换可执行文件名完成适配。采用 provider adapter 契约：

```text
Capabilities()       支持的模型、流输出、会话续接、审批、附件、工具状态等
BuildCommand()       生成固定 executable + argv + env allowlist + cwd
Start()              创建子进程并返回事件流
ParseEvent()         CLI 特有 JSONL/文本事件映射到内部事件
RespondApproval()    将 UI 审批转换为该 CLI 支持的确认/继续机制
ResumeOrRecover()    续接原 CLI session，或明确提示只能重新执行
Stop()               结束进程组并回收子进程
```

内部事件采用有限的公共模型：`message_delta`、`tool_started`、`tool_output`、`approval_required`、`question_required`、`plan_ready`、`usage`、`session_status`、`error`。保留 provider 原始事件用于诊断时必须做脱敏和大小限制。

- Claude Code adapter：通过官方支持的 CLI 非交互/流式输出与会话续接机制，先验证 JSON 输出事件、工具审批回调、权限模式及取消语义；复用 CLI 用户配置，不自行伪造 Claude SDK 协议。
- Codex adapter：通过 Codex CLI 可脚本化的 JSONL/exec 或 App Server 接口评估；优先选择官方稳定接口。验证增量输出、工具/审批事件、会话 resume、工作区沙箱参数和停止语义。
- 两者均需版本探测与兼容性检查；记录 CLI 版本、能力矩阵和启动失败原因。CLI 不存在、版本不兼容、认证缺失时提供设置页诊断。
- 适配器的首要可交付是启动、流式文本、停止和恢复；只有协议可表达且已测试的审批/工具事件才进入统一工作台。
- 不把 CLI 标准输出当作稳定协议；解析采用版本化 fixture、未知事件容忍和集成测试。

## 5. 交互流程

### 初次部署与配置

1. 管理员在 Linux 主机创建专用运行用户和工作目录，安装 Go 构建产物、Claude Code CLI 与 Codex CLI。
2. 配置允许访问的项目根目录、监听地址、持久化目录、TLS/代理信任及首个管理员凭据。
3. 从 SSH 隧道或 HTTPS 域名打开浏览器，完成首次管理员设置。
4. 系统检查每个 CLI 的路径、版本、认证状态和可用能力；凭据由 CLI 自身配置管理，除非后续明确启用加密凭据库。
5. 用户选择项目和 provider，创建会话并发送任务。

### 运行中的会话

1. 浏览器发起创建会话请求，服务端验证项目目录与 provider 参数，持久化会话后启动 adapter。
2. adapter 启动 CLI；服务端把 CLI 事件规范化，分配单调递增事件序号，落盘后广播给授权客户端。
3. 浏览器展示增量回复、工具活动、审批/问题卡和文件变更；审批动作经服务端重新验证当前待处理状态后转交 CLI。
4. 用户可停止任务、打开 PTY、查看 Git 状态和 Diff；任务运行与 WebSocket 连接生命周期解耦。
5. 网络恢复后客户端发送最后事件游标；服务端返回缺失事件或权威快照，再继续实时订阅。
6. 服务重启后按进程登记和 CLI session 能力执行恢复：能续接则续接；不能安全续接则标为中断待恢复，禁止无提示地重复执行有副作用任务。

## 6. 部署与使用

目标为 Linux x86_64/arm64，无 X11/Wayland 依赖。开发过程在浏览器完成，服务端可通过 SSH/VS Code Remote/本地浏览器访问。

建议部署形态：

- 初期提供静态 Go 二进制、示例 systemd unit 和可选 Dockerfile；容器运行时需挂载项目目录、CLI 配置目录和持久化目录，不能默认将敏感 HOME 烘焙进镜像。
- 服务监听回环或受控内网地址；公网部署使用 Nginx/Caddy HTTPS 反向代理并支持 WebSocket upgrade。
- 配置项至少包括 `CC_WEB_LISTEN`、`CC_WEB_DATA_DIR`、`CC_WEB_WORKSPACE_ROOTS`、`CC_WEB_AUTH_MODE`、`CC_WEB_TRUSTED_PROXIES`、`CC_WEB_CLAUDE_PATH`、`CC_WEB_CODEX_PATH`；具体名称实现时定稿。
- 更新前备份 SQLite 与配置；升级执行版本化迁移；提供健康检查、结构化日志和 CLI 子进程清理机制。
- 典型使用：启动服务 → 浏览器登录 → 选择项目 → 选 Codex/Claude → 发任务 → 审批工具调用 → 查看 Diff → 通过 Git 提交改动。

## 7. 主要风险与必须验证项

| 风险 | 缓解/验证 |
|---|---|
| CLI 输出/权限 API 随版本变化 | 锁定受测版本区间、fixture 回归、启动时版本检查；不兼容时禁用对应能力 |
| 不同 CLI 的审批/沙箱语义不等价 | 展示 provider 特定说明；统一最小权限默认值；危险能力仅显式开启 |
| 浏览器暴露 shell 与文件系统 | 根目录白名单、非 root、命令参数化、PTY 隔离、Origin/CSRF/认证、速率限制 |
| 远程认证与凭据泄漏 | 默认 loopback；HTTPS、独立会话密钥、密钥不进 URL/日志；限制反向代理信任 |
| 重启/断线导致重复任务或事件丢失 | 事件序号和持久化、进程登记、幂等恢复状态；未知副作用时转人工恢复 |
| 大仓库索引、超大 Diff、并发进程消耗资源 | 限制输出/上传/并发/扫描范围，分页与取消，资源配额和可观测指标 |
| CLI 认证依赖交互式登录或系统密钥环 | 在 headless Linux 实测官方认证方式；明确不支持的登录流程和备选环境变量/设备授权 |
| 许可证和品牌/代码复用边界 | 核对 cc-haha 许可证、NOTICE、第三方依赖及商标要求；新项目可先只复用交互概念和协议研究 |
| 多用户共享主机权限 | 首版限定可信单用户或管理员控制环境；多租户需额外做身份、文件隔离、配额、密钥隔离后才能启用 |

## 8. 建议实施阶段

1. 基础服务：Go 服务、配置/健康检查、认证边界、SQLite、项目根目录白名单、React 工作台壳。
2. 统一会话模型与实时协议：会话状态机、事件持久化、WebSocket 重连/快照、停止任务。
3. Claude Code adapter 与端到端 headless Linux 验证：流式回复、审批、恢复、进程清理。
4. Codex adapter：单独验证官方机器接口、事件、批准语义、resume 和沙箱，再进入 capability registry。
5. 工作区审阅：Git 状态、文件变更、Diff、逐文件审阅与受限撤销。
6. PTY、移动布局、预览代理及后续 Agent/Skills/MCP/定时任务能力。

每阶段均需单元测试、协议 fixture 测试和 Linux 集成测试；涉及真实 CLI 的测试使用临时 HOME 和临时仓库，不读取开发者真实配置，也不默认触发真实模型费用。

## 9. 实现前待确认

- 远程访问是否只限 VPN/SSH 隧道，还是需要公网 HTTPS 与多用户登录？
- 首期是否接受单用户自托管，并由 Linux 服务用户持有 CLI 认证？
- Codex 首选 App Server 还是 CLI JSONL/exec 路径，需要在目标 CLI 版本上做原型比较。
- Claude Code 与 Codex 的审批模式需要哪些安全默认项？是否允许跳过审批？
- 项目目录允许用户自行增加，还是只能管理员维护白名单？
- 新项目的许可协议与是否允许复用 cc-haha 代码/资产；品牌是否独立？
- 附件、图片、MCP 管理、工作树和定时任务是否属于首个可用版本？

## 10. 上游检查记录

本设计基于本地 `D:/WorkSpace/cc-haha` 当前检出版本分析。上游最近提交为 `14b1dd20`。桌面工作台视觉/交互结构参考 `README.md` 与 `docs/desktop/index.md`；运行时边界参考 `docs/internals/desktop.md` 和 `docs/internals/server.md`。本设计不宣称已完成 Claude/Codex CLI 集成；两种 CLI 的稳定流协议、审批与会话恢复需在目标 Linux/CLI 版本上先行验证。
