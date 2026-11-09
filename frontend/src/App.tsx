import { useEffect, useState, type FormEvent } from 'react';
import AuthScreen from './AuthScreen';
import { agentTeamsAdapter, api, AUTH_EXPIRED_EVENT, ApiError, clearAuthState, type ApiStatus, type AuthUser, type Project, type Session } from './api';
import type { AgentMember, AgentTeamSnapshot } from './types';

type IconName = 'panel' | 'search' | 'plus' | 'chevron' | 'terminal' | 'files' | 'send' | 'spark' | 'clock' | 'dots' | 'close' | 'back' | 'warning' | 'check' | 'moon';

function Icon({ name, size = 16 }: { name: IconName; size?: number }) {
  const common = { width: size, height: size, viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', strokeWidth: 1.7, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const, 'aria-hidden': true as const };
  const paths: Record<IconName, React.ReactNode> = {
    panel: <><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16"/></>,
    search: <><circle cx="11" cy="11" r="7"/><path d="m16 16 4 4"/></>,
    plus: <path d="M12 5v14M5 12h14"/>,
    chevron: <path d="m7 10 5 5 5-5"/>,
    terminal: <><path d="m4 17 5-5-5-5M12 19h8"/></>,
    files: <><path d="M3 6.5A1.5 1.5 0 0 1 4.5 5H10l2 2h7.5A1.5 1.5 0 0 1 21 8.5v10a1.5 1.5 0 0 1-1.5 1.5h-15A1.5 1.5 0 0 1 3 18.5z"/><path d="M3 9h18"/></>,
    send: <><path d="M12 19V5m-7 7 7-7 7 7"/></>,
    spark: <><path d="m12 3 1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8L12 3Z"/><path d="m19 16 .8 2.2L22 19l-2.2.8L19 22l-.8-2.2L16 19l2.2-.8L19 16Z"/></>,
    clock: <><circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/></>,
    dots: <><circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/></>,
    close: <path d="m6 6 12 12M18 6 6 18"/>,
    back: <path d="m15 18-6-6 6-6"/>,
    warning: <><path d="M12 3 2.8 19h18.4L12 3Z"/><path d="M12 9v4m0 3h.01"/></>,
    check: <path d="m5 12 4 4L19 6"/>,
    moon: <path d="M20.8 14.2A8.5 8.5 0 0 1 9.8 3.2 8.5 8.5 0 1 0 20.8 14.2Z"/>,
  };
  return <svg {...common}>{paths[name]}</svg>;
}

const seedMessages = [
  { id: 'm1', role: 'assistant', name: 'cc-web', time: '现在', text: '后端已连接。CLI runtime 尚未接入，所以消息可以写入本地服务状态，但不会启动 Codex、Claude 或 Agent Teams。' },
];

export default function App() {
  const [authLoading, setAuthLoading] = useState(true);
  const [authUser, setAuthUser] = useState<AuthUser | null>(null);
  const [authError, setAuthError] = useState('');
  const [authNotice, setAuthNotice] = useState('');
  const [snapshot, setSnapshot] = useState<AgentTeamSnapshot | null>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [sessions, setSessions] = useState<Session[]>([]);
  const [activeSessionId, setActiveSessionId] = useState('');
  const [apiStatus, setApiStatus] = useState<ApiStatus | null>(null);
  const [loadError, setLoadError] = useState('');
  const [navOpen, setNavOpen] = useState(false);
  const [teamOpen, setTeamOpen] = useState(true);
  const [teamPanelOpen, setTeamPanelOpen] = useState(false);
  const [plusOpen, setPlusOpen] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [selectedMember, setSelectedMember] = useState<AgentMember | null>(null);
  const [query, setQuery] = useState('');
  const [prompt, setPrompt] = useState('');
  const [messages, setMessages] = useState(seedMessages);
  const [toast, setToast] = useState('');
  const [theme, setTheme] = useState<'light' | 'dark'>('light');

  useEffect(() => {
    let mounted = true;
    api.authSession().then((user) => { if (mounted) setAuthUser(user); }).catch((error) => {
      if (mounted && !(error instanceof ApiError && error.status === 401)) setAuthError(error instanceof Error ? error.message : '无法验证登录状态。');
    }).finally(() => { if (mounted) setAuthLoading(false); });
    const onUnauthorized = () => {
      setAuthUser(null);
      setProjects([]);
      setSessions([]);
      setMessages([]);
      setActiveSessionId('');
      setSnapshot(null);
      setAuthError('登录状态已失效，请重新登录。');
      setAuthNotice('');
    };
    window.addEventListener(AUTH_EXPIRED_EVENT, onUnauthorized);
    return () => { mounted = false; window.removeEventListener(AUTH_EXPIRED_EVENT, onUnauthorized); };
  }, []);

  async function refreshWorkspace() {
    try {
      const [statusResponse, projectResponse] = await Promise.all([api.status(), api.projects()]);
      setApiStatus(statusResponse.status);
      setProjects(projectResponse.projects);
      const project = projectResponse.projects[0];
      if (!project) { setLoadError('当前 API 未配置任何允许访问的项目。'); return; }
      const sessionResponse = await api.sessions(project.id);
      setSessions(sessionResponse.sessions);
      const active = sessionResponse.sessions.find((item) => item.id === activeSessionId) ?? sessionResponse.sessions[0];
      if (active) {
        if (activeSessionId !== active.id) setActiveSessionId(active.id);
        const messageResponse = await api.messages(active.id);
        setMessages(messageResponse.messages.map((message) => ({ id: message.id, role: message.role === 'user' ? 'user' : 'agent', name: message.role === 'user' ? '你' : 'cc-web', time: new Date(message.createdAt).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }), text: message.content })));
        const teamState = await agentTeamsAdapter.refreshTeamPlan(active.id);
        setSnapshot(teamState);
      }
      setLoadError('');
    } catch (error) { setLoadError(error instanceof Error ? error.message : '无法连接 API。'); }
  }
  useEffect(() => { if (authUser && !authUser.mustChangePassword) void refreshWorkspace(); }, [authUser]);
  useEffect(() => { if (authUser && !authUser.mustChangePassword) void agentTeamsAdapter.getSnapshot(activeSessionId || null).then(setSnapshot); }, [activeSessionId, authUser]);
  useEffect(() => { if (authUser && !authUser.mustChangePassword && activeSessionId) void agentTeamsAdapter.refreshTeamPlan(activeSessionId).then((value) => { if (value.teamId) setSnapshot(value); }); }, [activeSessionId, authUser]);
  useEffect(() => {
    if (!toast) return;
    const timeout = window.setTimeout(() => setToast(''), 2800);
    return () => window.clearTimeout(timeout);
  }, [toast]);

  const filteredSessions = sessions.filter((session) => session.title.toLowerCase().includes(query.toLowerCase()));
  const activeSession = sessions.find((session) => session.id === activeSessionId) ?? null;
  const hasTeamPlan = Boolean(snapshot?.teamId);

  async function confirmArm() {
    if (!snapshot || !activeSessionId) { setToast('请先选择会话，再启用 Agent Teams。'); return; }
    try {
      const result = await agentTeamsAdapter.arm({ taskTitle: snapshot.taskTitle, sessionId: activeSessionId, acknowledgeAdditionalUsage: true, members: snapshot.members.map(({name,role,task}) => ({name,role,task:task.title})) });
      setSnapshot(result);
      setConfirmOpen(false);
      setPlusOpen(false);
      setToast('已武装下一条任务的 Agent Teams 模式；发送后会记录计划，runtime 尚未连接。');
    } catch (error) { setToast(error instanceof Error ? error.message : '无法启用 Agent Teams。'); }
  }

  async function submitMessage(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const text = prompt.trim();
    if (!text) return;
    if (!activeSessionId) { setToast('当前没有可写入的会话。'); return; }
    try {
      if (snapshot?.armed && snapshot.mode === 'team') {
        const teamResult = await agentTeamsAdapter.sendTeamMessage(activeSessionId, text, snapshot.members.map(({name,role,task}) => ({name,role,task:task.title})));
        setSnapshot(teamResult.snapshot);
        setToast(teamResult.message);
      } else {
        const result = await api.sendMessage(activeSessionId, text, false);
        setToast(result.notice);
      }
      setPrompt('');
      await refreshWorkspace();
    } catch (error) { setToast(error instanceof Error ? error.message : '发送失败。'); }
  }

  async function login(username: string, password: string) {
    const user = await api.login(username, password);
    setAuthError('');
    setAuthNotice('');
    setAuthUser(user);
  }

  async function changePassword(currentPassword: string, newPassword: string) {
    await api.changePassword(currentPassword, newPassword);
    setAuthUser(null);
    setAuthError('');
    setAuthNotice('密码已更新，请使用新密码重新登录。');
  }

  async function logout() {
    try { await api.logout(); } catch { /* Unauthorized responses already clear local state. */ }
    clearAuthState();
    setAuthUser(null);
    setProjects([]);
    setSessions([]);
    setMessages([]);
    setActiveSessionId('');
    setSnapshot(null);
  }

  if (authLoading) return <main className="auth-loading" aria-live="polite">正在验证登录状态…</main>;
  if (!authUser || authUser.mustChangePassword) return <AuthScreen key={authUser?.mustChangePassword ? 'change-password' : 'login'} username={authUser?.username} mustChangePassword={Boolean(authUser?.mustChangePassword)} initialError={authError} notice={authNotice} onLogin={login} onChangePassword={changePassword} />;

  return (
    <div className={`app-shell ${theme}`}>
      <header className="topbar">
        <div className="brand-cluster">
          <button className="icon-button mobile-nav-trigger" aria-label="打开导航" onClick={() => setNavOpen(true)}><Icon name="panel" /></button>
          <div className="brand-mark">cc</div><strong className="brand-name">cc-web</strong>
          <span className="top-divider" />
          <span className="project-crumb"><i className="project-dot" />cc-web <Icon name="chevron" size={13} /></span>
        </div>
        <div className="topbar-right">
          <span className="connection-pill"><i className="connection-dot" />{apiStatus ? `API ${apiStatus.api} · 数据库 ${apiStatus.persistence} · CLI ${apiStatus.cliRuntime}` : `API 未连接${loadError ? ` · ${loadError}` : ''}`}</span>
          <button className="icon-button theme-button" aria-label="切换主题" title="切换主题" onClick={() => setTheme(theme === 'light' ? 'dark' : 'light')}><Icon name="moon" /></button>
          <button className="user-menu" aria-label={`退出登录 ${authUser.username}`} title={`退出登录 ${authUser.username}`} onClick={() => void logout()}>{authUser.username.slice(0, 1).toUpperCase()}</button>
        </div>
      </header>

      <div className="workbench">
        <div className={`nav-scrim ${navOpen ? 'visible' : ''}`} onClick={() => setNavOpen(false)} />
        <aside className={`sidebar ${navOpen ? 'open' : ''}`}>
          <div className="sidebar-top">
            <button className="new-session" onClick={async () => { try { const project = projects[0]; if (!project) throw new Error('未配置可用项目。'); const created = await api.createSession(project.id, '新建会话', 'Codex', 'single'); await refreshWorkspace(); setActiveSessionId(created.session.id); setToast('会话已记录；CLI runtime 未连接，尚未执行任务。'); } catch (error) { setToast(error instanceof Error ? error.message : '无法创建会话。'); } }}><Icon name="plus" size={15} />新建会话 <kbd>⌘ N</kbd></button>
            <label className="search-field"><Icon name="search" size={14} /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索项目与会话" aria-label="搜索会话" /><kbd>⌘ K</kbd></label>
          </div>
          <div className="sidebar-scroll">
            <div className="side-label">项目 <button aria-label="添加项目" onClick={() => setToast('项目管理尚未连接。')}><Icon name="plus" size={14} /></button></div>
            {projects.map((project) => <button className={`project-row ${project.id === sessions.find((session) => session.id === activeSessionId)?.projectId ? 'active' : ''}`} key={project.id} onClick={() => { const first = sessions.find((session) => session.projectId === project.id); if (first) setActiveSessionId(first.id); }}><i className="folder-dot" /><span>{project.name}</span><span className="project-path">{project.path}</span></button>)}
            <div className="side-label sessions-label">最近会话 <button aria-label="会话菜单" onClick={() => setToast('会话操作需要后端连接。')}><Icon name="dots" size={14} /></button></div>
            {filteredSessions.map((session) => <button className={`session-row ${session.id === activeSessionId ? 'active' : ''}`} key={session.id} onClick={() => { setActiveSessionId(session.id); setNavOpen(false); }}><i className={`session-indicator ${session.id === activeSessionId ? 'active-indicator' : ''}`} /><span>{session.title}</span>{session.mode === 'team' && <span className="live-mark">Team</span>}</button>)}
            {filteredSessions.length === 0 && <div className="empty-search">没有匹配的会话</div>}
          </div>
          <div className="sidebar-footer"><span className="avatar-small">A</span><div><strong>本地预览</strong><small>单用户工作台</small></div><button className="icon-button" aria-label="用户菜单"><Icon name="dots" size={15} /></button></div>
        </aside>

        <main className="conversation-pane">
          <div className="conversation-header">
            <div className="conversation-heading"><span className="session-status" /><div><h1>{activeSession?.title ?? '选择会话'}</h1><span>{projects.find((project) => project.id === activeSession?.projectId)?.name ?? 'cc-web'} <span className="muted-separator">/</span> {activeSession?.provider ?? 'CLI'}（未连接）</span></div></div>
            <div className="conversation-actions"><span className="runtime-badge"><i />{apiStatus?.cliRuntime ?? '运行时未连接'}</span>{snapshot?.mode === 'team' && <span className="runtime-badge team-mode-badge">Agent Teams · 首发模式</span>}<button className="icon-button mobile-team-trigger" title="Agent Teams 面板" aria-label="打开 Agent Teams 面板" onClick={() => setTeamPanelOpen(true)}><Icon name="spark" /><span>团队</span></button><button className="icon-button" title="重新载入 API" aria-label="重新载入 API" onClick={() => void refreshWorkspace()}><Icon name="back" /></button></div>
          </div>

          <div className="conversation-scroll" id="conversation-scroll">
            <div className="day-marker"><span>{apiStatus?.persistence ?? 'API'} · 内存存储</span><i /></div>
            {messages.map((message) => <article className={`message ${message.role}`} key={message.id}>
              <div className={`message-avatar ${message.role === 'agent' ? 'agent-avatar' : ''}`}>{message.role === 'agent' ? <Icon name="spark" size={15} /> : '你'}</div>
              <div className="message-content"><div className="message-meta"><strong>{message.name}</strong><span>{message.time}</span>{message.role === 'agent' && <span>· gpt-5-codex（未连接）</span>}</div><div className={message.role === 'user' ? 'user-bubble' : 'agent-text'}>{message.text}</div>
                {message.id === 'message-welcome' && <div className="tool-card"><div className="tool-card-head"><span className="tool-icon"><Icon name="search" size={13} /></span><strong>API 状态 · CLI runtime 检查</strong><span className="tool-preview-status">未连接</span></div><div className="tool-card-body">当前服务只记录会话与计划状态；没有读取工作区文件，也没有启动 CLI。</div></div>}
              </div>
            </article>)}
            <section className="approval-preview"><div className="approval-icon"><Icon name="warning" size={15} /></div><div><strong>运行时未连接</strong><p>审批卡片与命令执行流程将在连接服务端后显示。当前没有等待审批的真实动作。</p></div><span className="preview-tag">预览态</span></section>
          </div>

          <div className="composer-dock">
            <form className="composer" onSubmit={submitMessage}>
              <textarea value={prompt} onChange={(event) => setPrompt(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter' && !event.shiftKey) { event.preventDefault(); event.currentTarget.form?.requestSubmit(); } }} rows={2} placeholder="描述任务；当前为本地 UI 预览…" aria-label="消息输入框" />
              <div className="composer-toolbar">
                <div className="composer-tools">
                  <button type="button" className={`tool-menu-trigger ${plusOpen ? 'selected' : ''}`} onClick={() => setPlusOpen(!plusOpen)} aria-haspopup="menu" aria-expanded={plusOpen}><Icon name="plus" size={15} /><span>能力</span><Icon name="chevron" size={12} /></button>
                  {snapshot?.armed ? <button type="button" className="provider-chip team-armed-chip" onClick={()=>setSnapshot({...snapshot,armed:false,mode:'single'})}><Icon name="spark" size={13}/>Agent Teams 已启用 <Icon name="close" size={12}/></button> : <span className="provider-chip"><span className="provider-glyph">✳</span>{activeSession?.provider ?? 'Codex'} <Icon name="chevron" size={11} /></span>}
                  <span className="permission-chip"><span className="shield-mark">◇</span>请求审批</span>
                </div>
                <div className="composer-submit"><span className="send-hint">Enter 发送 · Shift+Enter 换行</span><button className="send-button" aria-label="发送消息" type="submit" disabled={!prompt.trim()}><Icon name="send" size={15} /></button></div>
                {plusOpen && <div className="ability-menu" role="menu"><div className="ability-menu-title">扩展能力 <span>实验性</span></div><button role="menuitem" onClick={() => { setPlusOpen(false); setConfirmOpen(true); }}><span className="ability-icon"><Icon name="spark" size={15} /></span><span className="ability-copy"><strong>Agent Teams</strong><small>拆分任务并协同处理 · 实验性</small></span><Icon name="chevron" size={13} /></button><div className="ability-menu-foot">需要已配置并连接的 CLI runtime</div></div>}
              </div>
            </form>
            <div className="composer-footnote"><Icon name="warning" size={12} />本地 UI 预览 · 消息、文件与 Agent runtime 均未连接</div>
          </div>
        </main>

        <aside className={`team-panel ${teamPanelOpen ? 'mobile-open' : ''}`}>
          <div className="panel-header"><div><div className="panel-title-row"><h2>Agent Teams</h2><span className="experimental-badge">实验性</span></div><p>并行 Agent 运行概况</p></div><div className="panel-head-actions"><button className="icon-button mobile-team-close" aria-label="关闭团队面板" onClick={() => setTeamPanelOpen(false)}><Icon name="close" size={15} /></button><button className="icon-button collapse-team" aria-label="收起成员任务" onClick={() => setTeamOpen(!teamOpen)}><Icon name="chevron" size={15} /></button></div></div>
          <div className="runtime-notice"><span className="notice-symbol"><Icon name="warning" size={14} /></span><div><strong>运行依赖未连接</strong><p>{snapshot?.runtimeMessage ?? '正在读取本地预览状态…'}</p></div></div>
          <div className="team-summary"><div><span className="summary-label">计划状态</span><strong className="state-pill"><i />{snapshot?.teamStatus === 'awaiting_runtime' ? '已记录 · 待 runtime' : '草案 · 未启动'}</strong></div><div><span className="summary-label">成员</span><strong>0 / {snapshot?.members.length ?? 3} 运行</strong></div><div><span className="summary-label">模型用量</span><strong>未产生</strong></div></div>
          <div className="panel-divider" />
          <div className="member-list-header"><div><strong>成员任务</strong><span>{hasTeamPlan ? '服务端计划记录 · 成员未启动' : '分工草案 · 未分派'}</span></div><button className="text-button" onClick={() => setTeamOpen(!teamOpen)}>{teamOpen ? '收起' : '展开'} <Icon name="chevron" size={12} /></button></div>
          {teamOpen && <div className="member-list">
            {(snapshot?.members ?? []).map((member, index) => <div className="member-card" key={member.id}>
              <div className="member-top"><span className={`member-avatar member-color-${index}`}>{member.name.slice(0, 1)}</span><div className="member-identity"><strong>{member.name}</strong><span>{member.role}</span></div><span className="task-state">{snapshot?.teamStatus === 'awaiting_runtime' ? '已计划 · 未启动' : '未启动'}</span></div>
              <div className="task-row"><span className="task-bullet" /><span>{member.task.title}</span></div>
              <div className="member-card-foot"><span>{member.model}</span><button className="text-button" onClick={() => setSelectedMember(member)}>查看详情 <Icon name="chevron" size={12} /></button></div>
            </div>)}
          </div>}
          <div className="panel-bottom"><div className="last-sync"><Icon name="clock" size={13} /><span>数据持久层</span><strong>{apiStatus?.persistence ?? '未连接'}</strong></div><button className="connect-button" onClick={() => void refreshWorkspace()}>刷新 API <Icon name="chevron" size={14} /></button></div>
        </aside>
      </div>

      {confirmOpen && <div className="dialog-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setConfirmOpen(false); }}><section className="confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="confirm-title"><div className="dialog-kicker"><span className="dialog-icon"><Icon name="spark" size={17} /></span><span>实验性能力 · Agent Teams</span><button className="icon-button close-dialog" aria-label="关闭确认" onClick={() => setConfirmOpen(false)}><Icon name="close" size={17} /></button></div><h2 id="confirm-title">启用并行 Agent？</h2><p className="dialog-lede">该能力可能让多个 Agent 同时处理任务。启动前请确认你理解额外用量和协调成本。</p><div className="cost-callout"><div className="cost-callout-icon"><Icon name="warning" size={16} /></div><div><strong>可能产生额外模型用量</strong><p>并行成员可能各自消耗模型 tokens；总用量可能高于单 Agent。当前未接入用量估算，无法提供可靠金额或上限，请先确认你的账户计费设置。</p></div></div><div className="task-context"><span>当前任务</span><strong>{snapshot?.taskTitle ?? '修复登录态恢复问题'}</strong><small>成员数：{snapshot?.members.length ?? 3}（分工草案，尚未分派）</small></div><div className="dialog-runtime"><i />Runtime 未连接。确认后只会记录本地演示状态，不会启动 Agent。</div><div className="dialog-actions"><button className="cancel-button" onClick={() => setConfirmOpen(false)}>暂不启用</button><button className="confirm-button" onClick={() => void confirmArm()}><Icon name="check" size={15} />我理解额外用量，继续</button></div><p className="dialog-disclaimer">确认不会启动任何工作；真实执行需连接并支持 Agent Teams 的 Provider。</p></section></div>}

      {selectedMember && <div className="dialog-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setSelectedMember(null); }}><section className="detail-dialog" role="dialog" aria-modal="true" aria-labelledby="member-title"><div className="detail-header"><button className="back-button" onClick={() => setSelectedMember(null)}><Icon name="back" size={16} />返回成员列表</button><button className="icon-button" aria-label="关闭详情" onClick={() => setSelectedMember(null)}><Icon name="close" size={17} /></button></div><div className="detail-member"><span className="member-avatar member-color-1">{selectedMember.name.slice(0, 1)}</span><div><h2 id="member-title">{selectedMember.name}</h2><p>{selectedMember.role} · {selectedMember.model}</p></div><span className="task-state">{snapshot?.teamStatus === 'awaiting_runtime' ? '已计划 · 未启动' : '未启动'}</span></div><div className="detail-section"><span className="summary-label">任务</span><h3>{selectedMember.task.title}</h3><p>{selectedMember.task.detail}</p></div><div className="detail-section"><span className="summary-label">运行状态</span><div className="detail-runtime"><i />未启动 · Agent Teams runtime 未连接</div></div><div className="detail-section"><span className="summary-label">事件</span><div className="empty-events"><Icon name="clock" size={16} />尚无运行事件。此成员只是 UI 演示分工，尚未创建真实 Agent 任务。</div></div></section></div>}
      {toast && <div className="toast" role="status" aria-live="polite">{toast}</div>}
    </div>
  );
}
