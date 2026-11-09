import type { AgentTeamsAdapter, AgentTeamSnapshot, ArmTeamRequest } from './types';

export interface Project { id: string; name: string; path: string }
export interface Session { id: string; projectId: string; title: string; provider: string; mode: string; status: string; createdAt: string }
export interface Message { id: string; sessionId: string; role: string; content: string; createdAt: string }
export interface ApiStatus { api: string; cliRuntime: string; teamRuntime: string; persistence: 'sqlite' | string }
const API = (import.meta.env.VITE_API_BASE ?? import.meta.env.BASE_URL).replace(/\/+$/, '');
export const AUTH_EXPIRED_EVENT = 'cc-web-auth-expired';
let csrfToken = '';

export interface AuthUser { username: string; mustChangePassword: boolean }
interface AuthResponse { user: { username: string }; csrfToken: string; mustChangePassword: boolean }
export class ApiError extends Error {
  constructor(message: string, readonly status: number, readonly code?: string) { super(message); }
}

export function clearAuthState() { csrfToken = ''; }

async function request<T>(path: string, init?: RequestInit, notifyUnauthorized = true): Promise<T> {
  let response: Response;
  const headers = new Headers(init?.headers);
  if (init?.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json');
  if (init?.method && !['GET', 'HEAD', 'OPTIONS'].includes(init.method.toUpperCase()) && csrfToken) headers.set('X-CSRF-Token', csrfToken);
  try { response = await fetch(`${API}${path}`, { ...init, credentials: 'include', headers }); }
  catch { throw new Error('无法连接 cc-web 后端。请确认 backend 服务正在运行。'); }
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    if (response.status === 401 && notifyUnauthorized) {
      clearAuthState();
      window.dispatchEvent(new Event(AUTH_EXPIRED_EVENT));
    }
    throw new ApiError(body?.error?.message ?? `HTTP ${response.status}`, response.status, body?.error?.code);
  }
  return body as T;
}
function mapAuthResponse(response: AuthResponse): AuthUser {
  csrfToken = response.csrfToken;
  return { username: response.user.username, mustChangePassword: response.mustChangePassword };
}
export const api = {
  authSession: async () => mapAuthResponse(await request<AuthResponse>('/api/auth/me', undefined, false)),
  login: async (username: string, password: string) => mapAuthResponse(await request<AuthResponse>('/api/auth/login', { method: 'POST', body: JSON.stringify({ username, password }) }, false)),
  changePassword: async (currentPassword: string, newPassword: string) => {
    await request<void>('/api/auth/password', { method: 'POST', body: JSON.stringify({ currentPassword, newPassword }) }, false);
    clearAuthState();
  },
  logout: async () => {
    try { await request<void>('/api/auth/logout', { method: 'POST' }); }
    finally { clearAuthState(); }
  },
  status: () => request<{ status: ApiStatus }>('/api/status'),
  projects: () => request<{ projects: Project[] }>('/api/projects'),
  sessions: (projectId: string) => request<{ sessions: Session[] }>(`/api/sessions?projectId=${encodeURIComponent(projectId)}`),
  createSession: (projectId: string, title: string, provider: string, mode: string) => request<{session:Session;runtimeConnected:boolean;message:string}>('/api/sessions', {method:'POST',body:JSON.stringify({projectId,title,provider,mode})}),
  messages: (sessionId: string) => request<{ messages: Message[] }>(`/api/sessions/${encodeURIComponent(sessionId)}/messages`),
  sendMessage: (sessionId: string, content: string, teamMode = false) => request<{ message: Message; notice: string }>(`/api/sessions/${encodeURIComponent(sessionId)}/messages`, { method: 'POST', body: JSON.stringify({ content, teamMode }) }),
  createTeamPlan: (sessionId: string, taskPrompt: string, members: Array<{name:string;role:string;task:string}>) => request<{ teamRun: { id:string; sessionId:string; taskTitle:string; taskPrompt:string; status:string; members:Array<{id:string;name:string;role:string;status:string;currentTask:string}> }; notice:string }>(`/api/sessions/${encodeURIComponent(sessionId)}/team-runs`, { method: 'POST', body: JSON.stringify({ taskPrompt, members }) }),
  sendTeamMessage: (teamId: string, content: string) => request<{ notice:string; teamRun:{ id:string; status:string } }>(`/api/team-runs/${encodeURIComponent(teamId)}/messages`, { method:'POST', body:JSON.stringify({ content }) }),
};

const demoMembers = [
  { id: 'review', name: '审阅 Agent', role: '方案审阅', model: '能力未探测', status: 'not_started' as const, task: { id: 'review-task', title: '检查刷新与过期处理边界', detail: '评估 token 刷新失败时的状态清理与重试路径。当前为计划草案，尚未分派。', status: 'not_started' as const } },
  { id: 'tests', name: '测试 Agent', role: '测试设计', model: '能力未探测', status: 'not_started' as const, task: { id: 'test-task', title: '补充登录态刷新测试用例', detail: '覆盖静默刷新成功、刷新失败和并发请求等场景。当前为计划草案，尚未执行。', status: 'not_started' as const } },
  { id: 'docs', name: '文档 Agent', role: '变更摘要', model: '能力未探测', status: 'not_started' as const, task: { id: 'docs-task', title: '整理安全影响与验收项', detail: '整理供主 Agent 审阅的验收清单。当前为计划草案，尚未创建运行任务。', status: 'not_started' as const } },
];
function createSnapshot(sessionId: string | null): AgentTeamSnapshot { return { teamId:null, teamStatus:'draft', runtime:'disconnected', runtimeMessage:'Go API 可用；Agent Teams CLI runtime 尚未连接。成员列表为计划草案。', costConfirmed:false, taskTitle:'优化登录态刷新与过期提示', updatedAt:'本地计划', sessionId, armed:false, mode:'single', members:demoMembers }; }
export const agentTeamsAdapter: AgentTeamsAdapter = {
  async getSnapshot(sessionId) { return sessionId ? this.refreshTeamPlan(sessionId) : createSnapshot(null); },
  async arm(request: ArmTeamRequest): Promise<AgentTeamSnapshot> {
    if (!request.sessionId) throw new Error('当前项目没有可用会话。');
    return {...createSnapshot(request.sessionId),costConfirmed:true,armed:true,mode:'team'};
  },
  async sendTeamMessage(sessionId, content, members) {
    const existing = await request<{teamRuns:Array<{id:string;status:string;members:Array<{id:string;name:string;role:string;status:string;currentTask:string}>}>}>(`/api/sessions/${encodeURIComponent(sessionId)}/team-runs`);
    const current = existing.teamRuns.at(-1);
    const plan = current ? { teamRun:current, notice:'沿用当前团队计划。' } : await api.createTeamPlan(sessionId, content, members);
    const result = await api.sendTeamMessage(plan.teamRun.id, content);
    return { message:`${plan.notice} ${result.notice}`, snapshot:{...createSnapshot(sessionId),teamId:plan.teamRun.id,teamStatus:result.teamRun.status,costConfirmed:true,armed:false,mode:'team',members:plan.teamRun.members.map(m=>({...demoMembers.find(d=>d.name===m.name)!,id:m.id,status:'not_started'}))} };
  },
  async refreshTeamPlan(sessionId) {
    const { teamRuns } = await request<{teamRuns:Array<{id:string;status:string;members:Array<{id:string;name:string;role:string;status:string;currentTask:string}>}>}>(`/api/sessions/${encodeURIComponent(sessionId)}/team-runs`);
    const run = teamRuns.at(-1);
    if (!run) return createSnapshot(sessionId);
    return {...createSnapshot(sessionId),teamId:run.id,teamStatus:run.status,costConfirmed:true,mode:'team',members:run.members.map(m=>({...demoMembers.find(d=>d.name===m.name)!,id:m.id,status:'not_started'}))};
  },
};
