export type TeamRuntimeState = 'disconnected' | 'connecting' | 'ready' | 'error';
export type AgentTaskState = 'not_started' | 'queued' | 'running' | 'blocked' | 'completed' | 'failed';

export interface AgentTask {
  id: string;
  title: string;
  detail: string;
  status: AgentTaskState;
}

export interface AgentMember {
  id: string;
  name: string;
  role: string;
  model: string;
  status: AgentTaskState;
  task: AgentTask;
}

export interface AgentTeamSnapshot {
  teamId: string | null;
  teamStatus?: string;
  runtime: TeamRuntimeState;
  runtimeMessage: string;
  costConfirmed: boolean;
  taskTitle: string;
  members: AgentMember[];
  updatedAt: string;
  sessionId: string | null;
  armed: boolean;
  mode: 'single' | 'team';
}

export interface ArmTeamRequest {
  taskTitle: string;
  sessionId: string;
  acknowledgeAdditionalUsage: true;
  members: Array<{ name: string; role: string; task: string }>;
}

export interface AgentTeamsAdapter {
  getSnapshot(sessionId: string | null): Promise<AgentTeamSnapshot>;
  arm(request: ArmTeamRequest): Promise<AgentTeamSnapshot>;
  sendTeamMessage(sessionId: string, content: string, members: ArmTeamRequest['members']): Promise<{ message: string; snapshot: AgentTeamSnapshot }>;
  refreshTeamPlan(sessionId: string): Promise<AgentTeamSnapshot>;
}
