// TypeScript types matching the Phase 3 REST API schemas.
// See docs/specs/phase-3-rest-api-crud.md for field details.

export interface AgentConfig {
  name: string;
  description: string;
  system_prompt: string;
  tools: string[];
  max_step: number;
}

export interface MCPConfig {
  name: string;
  transport: 'inproc' | 'stdio';
  enabled_if: string;
  // inproc only
  provider?: string;
  default_root?: string;
  // stdio only
  command?: string;
  args?: string[];
  env?: Record<string, string>;
  init_timeout?: string;
}

export interface ToolInfo {
  name: string;
  description: string;
  used_by: string[];
}
