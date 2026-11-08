# cc-web 设计方案

状态：实现进行中；当前已完成 HTTP API + React 工作台控制面首个纵切片。真实 CLI 执行、持久化、公网认证和完整桌面适用能力仍未交付。

## 1. 目标与原则

`cc-web` 是面向 Linux 无图形环境的自托管 Web Agent 工作台。服务进程运行在 Linux 主机，浏览器负责交互；项目文件、CLI 进程、凭据和任务均留在主机。首期支持 Codex CLI 与 Claude Code CLI，通过 provider adapter 隔离各自的启动参数、流事件、会话恢复与审批语义。

设计目标：

- 复用 `cc-haha` 的工作台心智模型：左侧项目/会话导航，中间对话与任务状态，右侧文件变更/Diff/终端面板。
- 将所有执行和授权集中在服务端；浏览器不直接访问文件系统或 CLI。
- 断线后任务继续运行，重连后从服务端状态和事件游标恢复。
- 优先实现可靠的会话生命周期、审批、文件审阅和终端体验，再逐步扩展桌面版高级能力。
- 首期按单用户自托管设计；Linux 服务用户持有并使用本机 CLI 配置。暂不提供多租户账号体系，避免把“有登录页”误当成完整隔离能力。
- 用户确认在设置中选择运行 CLI（Codex CLI 或 Claude Code CLI）；界面提供 provider 选择，后端仍按各自 adapter 语义执行。

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

### H5 与完整 Web 工作台的区别

`cc-haha` 已有的 H5/手机端是与桌面运行时配对的远程控制客户端：任务和凭据留在配对电脑，页面侧重审批、派发、查看进度；并非一台独立的 Linux Web Agent 主机。它没有远程终端，也不包含 Provider/MCP/技能管理、Computer Use 和原生浏览器。cc-web 则把 Web 服务与 CLI 一起部署在 Linux 主机，浏览器成为主要工作台，终端及文件/命令权限因此进入新的远程攻击面。界面可复用桌面版的导航、会话标签、对话时间线、工具卡、审批卡、输入工具栏和右侧工作区模式；移动布局参考 H5 的会话/审批优先设计，但不照搬其“已配对电脑”拓扑。

## 3. 界面设计评审基线

### 共用设计语义

- 默认采用桌面版「纯白 pure-white」主题：页面 `#FFFFFF`、导航底 `#F7F7F6`、悬停/选中 `#F1F0EE`、嵌套面 `#F5F5F3`、分割线 `#E9E8E5`、主文字 `#1A1917`、次级文字 `#494640` / `#625E58`、品牌陶土色 `#A24A2E`。按钮/选中用品牌色；运行状态独立用成功绿、警告琥珀、错误红、信息蓝，禁止把所有状态染成品牌色。
- 桌面参考实现具备六套主题：纯白 `white`、纸墨 `paper`、暖经典 `warm-classic`、青瓷 `celadon`、墨夜 `dark`、墨蓝 `ink-blue`。方向初稿可提供简化主题切换，但首态统一纯白，保证版式横向比较。
- 延续紧凑工作台排版、细分割线、浅中性色层级；避免营销页构图、夸张渐变、装饰性图标和仅用于装饰的统计数字。正文达到网页可读性要求，移动主要点击目标不小于 44px。

### 同一演示场景

所有视觉方向使用同一演示数据以便比较：项目 `cc-web`；会话 `修复登录态恢复问题`；Provider 可切换 Codex / Claude；当前会话显示用户问题、Agent 回复、一个执行中的文件读取/命令过程、一次待审批的修改动作，以及变更文件列表和单文件 Diff。将样例标识为原型演示内容，不伪称实际任务、运行耗时或统计数据。

### 必须能评审的交互

- 在项目与历史会话间切换或打开会话导航；搜索框可输入并展示/筛选演示结果。
- 新建会话入口、Codex/Claude 选择、发送消息与停止运行状态入口。
- 审批卡明确操作来源、动作影响和权限边界；批准/拒绝后有可见状态变化。
- 工作区与 Diff 面板能切换/展开查看；最少一处变更行以新增、删除语义底色区分。
- 主题切换、菜单/移动抽屉均有可见交互状态；无死按钮。

### 桌面与移动适配验收

- PC：呈现对话核心和至少一个工作区审阅入口，同时可访问项目/会话导航；约 1440px 视口结构稳定。
- 手机：以对话/待审批动作优先；导航与工作区采用抽屉、底部面板或显式分区切换，不将桌面多栏等比例挤窄。375px 下不出现横向整页溢出，审批动作易触达、Diff 可横向查看或逐行可读。
- 交付物要包含方向缩略图比较页、三个完整 HTML 方向、对应截图、方向卡及方向 gate 文件；当前阶段停在初稿评审，待用户选定后再做最终实现。

## 4. 系统架构

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
- 用户已选择公网 HTTPS 访问。服务默认仍只绑定 `127.0.0.1`，由 Nginx/Caddy 等 HTTPS 反向代理对外提供入口；不建议 Go 服务直接裸露公网端口。公网开放前必须启用强管理员认证（单用户不等于免认证）、安全 Cookie、登录限速、可信 Origin、CSRF 防护、WebSocket 同源鉴权、独立随机密钥和安全响应头；不把 CORS 当认证。

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
- 本项目已确定公网 HTTPS 部署。Go 服务绑定回环地址，仅由 Nginx/Caddy 反代；配置 WebSocket upgrade、TLS 自动续期、可信代理头、强管理员认证、登录限速、会话撤销、审计日志和备份。公网开放前完成威胁建模及安全测试。
- 配置项至少包括 `CC_WEB_LISTEN`、`CC_WEB_DATA_DIR`、`CC_WEB_WORKSPACE_ROOTS`、`CC_WEB_AUTH_MODE`、`CC_WEB_TRUSTED_PROXIES`、`CC_WEB_CLAUDE_PATH`、`CC_WEB_CODEX_PATH`；具体名称实现时定稿。
- 更新前备份 SQLite 与配置；升级执行版本化迁移；提供健康检查、结构化日志和 CLI 子进程清理机制。
- 典型使用：启动服务 → 浏览器登录 → 选择项目 → 选 Codex/Claude → 发任务 → 审批工具调用 → 查看 Diff → 通过 Git 提交改动。

## 7. 主要风险与必须验证项

| 风险 | 缓解/验证 |
|---|---|
| CLI 输出/权限 API 随版本变化 | 锁定受测版本区间、fixture 回归、启动时版本检查；不兼容时禁用对应能力 |
| 不同 CLI 的审批/沙箱语义不等价 | 展示 provider 特定说明；统一最小权限默认值；危险能力仅显式开启；禁止用兼容层把两者伪装成相同保证 |
| 浏览器暴露 shell 与文件系统 | 根目录白名单、非 root、命令参数化、PTY 隔离、Origin/CSRF/认证、速率限制 |
| 远程认证与凭据泄漏 | 已确定公网 HTTPS：Go 仅监听 loopback，使用 Nginx/Caddy TLS、强管理员认证、登录限速、Origin/CSRF 校验、独立会话密钥且密钥不进 URL/日志，并限制可信反代。Web 服务直接承载可执行 CLI 的主机权限，风险高于 cc-haha 受限 H5 网关；公网开放前必须威胁建模和安全测试 |
| 重启/断线导致重复任务或事件丢失 | 事件序号和持久化、进程登记、幂等恢复状态；未知副作用时转人工恢复 |
| 大仓库索引、超大 Diff、并发进程消耗资源 | 限制输出/上传/并发/扫描范围，分页与取消，资源配额和可观测指标 |
| CLI 认证依赖交互式登录或系统密钥环 | 在 headless Linux 实测官方认证方式；明确不支持的登录流程和备选环境变量/设备授权 |
| 许可证和品牌/代码复用边界 | 用户希望参考桌面版功能与视觉；该表述不等于权利人书面许可。当前 cc-haha/LICENSE 为 Community Source License 1.0，限制商业分发、付费 SaaS/托管、付费客户工作和收费二开/集成/维护。未取得适用的书面商业许可前，cc-web 独立实现功能与 UI，不复制其受限源代码；如未来分发复用内容，须附完整 LICENSE/NOTICE/第三方声明及要求的署名，并逐项核对依赖与资源许可 |
| 多用户共享主机权限 | 首版限定可信单用户或管理员控制环境；多租户需额外做身份、文件隔离、配额、密钥隔离后才能启用 |

## 8. 建议实施阶段

1. 基础服务：Go 服务、配置/健康检查、SQLite migrations 与 Docker named volume、React 工作台壳；管理员认证、项目根目录白名单仍待实现。
2. 统一会话模型与实时协议：会话状态机、事件序号/持久事件、WebSocket 重连/快照、停止任务。
3. Claude Code adapter 与端到端 headless Linux 验证：流式回复、审批、恢复、进程清理。
4. Codex adapter：单独验证官方机器接口、事件、批准语义、resume 和沙箱，再进入 capability registry。
5. 工作区审阅：Git 状态、文件变更、Diff、逐文件审阅与受限撤销。
6. PTY、移动布局、预览代理及后续 Agent/Skills/MCP/定时任务能力。

每阶段均需单元测试、协议 fixture 测试和 Linux 集成测试；涉及真实 CLI 的测试使用临时 HOME 和临时仓库，不读取开发者真实配置，也不默认触发真实模型费用。

## 9. 实施状态与下一步门禁

### 已落地的正式切片

- `backend/`：可启动的 Go HTTP API，提供 health/status、项目/会话、消息、Team 计划和 Team 消息、审批记录接口。Team 计划只能进入 `awaiting_runtime`，成员为 `planned`，消息状态为 `not_dispatched`，审批响应始终为 `execution=not_executed`。
- `frontend/`：React + TypeScript + Vite 工作台控制面，含项目/会话导航、会话读取与发送、团队成本确认、首条 Team 模式武装、计划成员列表与详情入口、桌面三栏和手机面板。
- 数据层已改为纯 Go SQLite 文件存储（`CC_WEB_DB_PATH`，Docker 默认 `/data/cc-web.db`），支持 schema migration；Compose 用 named volume 跨容器重建保留数据。`/api/status` 报告 `persistence=sqlite`。目前没有事件序号/流式事件表，也不包含 CLI 执行、CLI 审批、文件访问、Git/Diff、PTY、WebSocket 流或真实 Agent 成员。UI 和 API 均须继续显示 runtime disconnected，不能把计划/审批意图写成执行成功。

### 下一阶段阻塞门禁

1. 在目标 Linux headless 环境，以临时 HOME 与临时仓库固定一个 Codex 或 Claude CLI 版本，验证安装、非交互认证、事件协议、权限/审批、停止、会话恢复和进程组清理；未通过前不开放 CLI 能力。
2. SQLite migrations 与 Docker named volume 已接入；下一步补事件序号/持久事件、管理员认证、CSRF/Origin/会话校验、项目根目录 canonical path/symlink allowlist、速率限制与审计，公网 HTTPS 开放前完成威胁建模。
3. 已确认外网 HTTPS 目标，但现阶段 Compose 只发布宿主机 loopback；缺少认证与工作区 allowlist，禁止互联网/共享网络部署。Docker image/Compose 还需在具备 Docker daemon 的 Linux CI 实际 build/up 验证，并制定定期 SQLite 一致性备份与恢复演练。
4. Agent Teams 为产品明确要求的目标能力，但具体 CLI 运行能力未验证。首个控制面只能记录计划；正式执行须先通过 provider team spawn/通信/成员 transcript/停止/恢复 spike，再实现 plan review/approve 与团队状态 API。
5. cc-haha 当前许可为 Community Source License 1.0。个人/非商业及限定组织内部自用不等同商业交付许可；付费客户工作、收费 SaaS/托管、商业分发及收费二开需适用权利人书面商业许可。未获许可前保持独立实现，不复制当前受限实现源码。


## 10. 上游检查记录

本设计基于本地 `D:/WorkSpace/cc-haha` 当前检出版本分析。上游最近提交为 `14b1dd20`。桌面工作台视觉/交互结构参考 `README.md` 与 `docs/desktop/index.md`；运行时边界参考 `docs/internals/desktop.md` 和 `docs/internals/server.md`。本设计不宣称已完成 Claude/Codex CLI 集成；两种 CLI 的稳定流协议、审批与会话恢复需在目标 Linux/CLI 版本上先行验证。
