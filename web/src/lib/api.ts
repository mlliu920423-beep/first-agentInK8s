// REST API client for Phase 3 endpoints.
// Simple fetch-based; no axios/tanstack-query dependency.
import type { AgentConfig, MCPConfig } from './types';

const BASE = '/api';

async function handleResponse(res: Response): Promise<void> {
  if (!res.ok) {
    const text = await res.text().catch(() => 'unknown error');
    throw new Error(`${res.status}: ${text}`);
  }
}

// Agents ----------------------------------------------------------------------

export async function listAgents(): Promise<AgentConfig[]> {
  const res = await fetch(`${BASE}/agents`);
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

export async function createAgent(cfg: AgentConfig): Promise<void> {
  const res = await fetch(`${BASE}/agents`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(cfg),
  });
  await handleResponse(res);
}

export async function updateAgent(name: string, cfg: AgentConfig): Promise<void> {
  const res = await fetch(`${BASE}/agents/${encodeURIComponent(name)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(cfg),
  });
  await handleResponse(res);
}

export async function deleteAgent(name: string): Promise<void> {
  const res = await fetch(`${BASE}/agents/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  });
  await handleResponse(res);
}

// MCP servers -----------------------------------------------------------------

export async function listMCPs(): Promise<MCPConfig[]> {
  const res = await fetch(`${BASE}/mcp`);
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

export async function createMCP(cfg: MCPConfig): Promise<void> {
  const res = await fetch(`${BASE}/mcp`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(cfg),
  });
  await handleResponse(res);
}

export async function updateMCP(name: string, cfg: MCPConfig): Promise<void> {
  const res = await fetch(`${BASE}/mcp/${encodeURIComponent(name)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(cfg),
  });
  await handleResponse(res);
}

export async function deleteMCP(name: string): Promise<void> {
  const res = await fetch(`${BASE}/mcp/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  });
  await handleResponse(res);
}
