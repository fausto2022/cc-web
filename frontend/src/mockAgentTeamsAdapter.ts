import { agentTeamsAdapter } from './api';

// Compatibility export for older imports; all state is now backed by the Go API.
export const mockAgentTeamsAdapter = agentTeamsAdapter;
