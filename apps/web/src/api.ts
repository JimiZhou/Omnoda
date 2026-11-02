export type NodeInfo = {
  id: string;
  display_name: string;
  os: string;
  arch: string;
  version: string;
  hostname: string;
  status: string;
  labels: Record<string, string>;
  capabilities: { name: string; present: boolean; detail?: string }[];
  harnesses: { id: string; present: boolean; version?: string; path?: string; error?: string }[];
  workspaces: string[];
  last_seen?: string;
};

let csrf = "";

export function setCSRF(token: string) {
  csrf = token;
  sessionStorage.setItem("omnoda_csrf", token);
}

export function loadCSRF() {
  csrf = sessionStorage.getItem("omnoda_csrf") || "";
}

async function req<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body && !headers.has("Content-Type")) headers.set("Content-Type", "application/json");
  if (csrf && init.method && init.method !== "GET") headers.set("X-CSRF-Token", csrf);
  const res = await fetch(path, { ...init, headers, credentials: "same-origin" });
  const text = await res.text();
  const data = text ? JSON.parse(text) : {};
  if (!res.ok) {
    throw new Error(data.message || res.statusText);
  }
  return data as T;
}

export const api = {
  meta: () => req<{ setup_required: boolean; version: string; public_url: string }>("/api/v1/meta"),
  setup: (username: string, password: string) =>
    req("/api/v1/setup", { method: "POST", body: JSON.stringify({ username, password }) }),
  login: async (username: string, password: string) => {
    const r = await req<{ csrf: string; username: string }>("/api/v1/auth/login", {
      method: "POST",
      body: JSON.stringify({ username, password }),
    });
    setCSRF(r.csrf);
    return r;
  },
  me: () => req<{ user_id: string; csrf: string }>("/api/v1/auth/me"),
  logout: () => req("/api/v1/auth/logout", { method: "POST", body: "{}" }),
  nodes: () => req<{ nodes: NodeInfo[] }>("/api/v1/nodes"),
  node: (id: string) => req<NodeInfo>(`/api/v1/nodes/${id}`),
  revoke: (id: string) => req(`/api/v1/nodes/${id}`, { method: "DELETE" }),
  enroll: (display_name: string, os: string) =>
    req<{ code: string; install_sh: string; install_ps: string; expires_at: string }>(
      "/api/v1/nodes/enrollment-codes",
      { method: "POST", body: JSON.stringify({ display_name, os }) }
    ),
  task: (body: Record<string, unknown>) =>
    req<{ task_id: string; status: string }>("/api/v1/tasks", { method: "POST", body: JSON.stringify(body) }),
  tasks: () => req<{ tasks: TaskRow[] }>("/api/v1/tasks"),
  taskDetail: (id: string) => req<{ task: TaskRow; spec: Record<string, unknown> }>(`/api/v1/tasks/${id}`),
  events: (id: string) => req<{ events: EventRow[] }>(`/api/v1/tasks/${id}/events`),
  cancelTask: (id: string) => req(`/api/v1/tasks/${id}/cancel`, { method: "POST", body: "{}" }),
  workflow: (goal: string, planner: string) =>
    req<{ workflow_id: string; planner: string; plan: Plan }>("/api/v1/workflows", {
      method: "POST",
      body: JSON.stringify({ goal, planner }),
    }),
  workflows: () => req<{ workflows: WfRow[] }>("/api/v1/workflows"),
  workflowDetail: (id: string) =>
    req<{ workflow: WfRow; plan: Plan; steps: StepRow[]; report: Report | null }>(`/api/v1/workflows/${id}`),
  cancelWorkflow: (id: string) => req(`/api/v1/workflows/${id}/cancel`, { method: "POST", body: "{}" }),
  approvals: () => req<{ approvals: Approval[] }>("/api/v1/approvals?status=pending"),
  decide: (id: string, ok: boolean) =>
    req(`/api/v1/approvals/${id}/${ok ? "approve" : "reject"}`, { method: "POST", body: "{}" }),
  audit: () => req<{ audit: Audit[] }>("/api/v1/audit"),
  model: () => req<Model>("/api/v1/settings/model"),
  saveModel: (m: Model) => req<Model>("/api/v1/settings/model", { method: "PUT", body: JSON.stringify(m) }),
  capabilities: () => req<{ capabilities: Capability[] }>("/api/v1/capabilities"),
  evolutions: () => req<{ evolutions: Evolution[] }>("/api/v1/evolutions"),
  approveEvolution: (id: string) => req<Evolution>(`/api/v1/evolutions/${id}/approve`, { method: "POST", body: "{}" }),
  rejectEvolution: (id: string) => req<Evolution>(`/api/v1/evolutions/${id}/reject`, { method: "POST", body: "{}" }),
  disableCapability: (id: string) => req(`/api/v1/capabilities/${id}/disable`, { method: "POST", body: "{}" }),
  agentRun: (goal: string, workspace: string) =>
    req<{ answer: string }>("/api/v1/agent/run", { method: "POST", body: JSON.stringify({ goal, workspace }) }),
  conversations: () => req<{ conversations: Conversation[] }>("/api/v1/conversations"),
  createConversation: (title: string) =>
    req<Conversation>("/api/v1/conversations", { method: "POST", body: JSON.stringify({ title }) }),
  conversation: (id: string) =>
    req<{ conversation: Conversation; messages: Msg[] }>(`/api/v1/conversations/${id}`),
  postMessage: (id: string, content: string, idempotency_key: string) =>
    req<{ conversation_id: string; task_id: string; status: string; idempotent?: boolean }>(
      `/api/v1/conversations/${id}/messages`,
      { method: "POST", body: JSON.stringify({ content, idempotency_key }) },
    ),
};

export type TaskRow = {
  ID: string; NodeID: string; Kind: string; Status: string; Summary: string; Output: string; Error: string; ExitCode: number; WorkflowID: string; StepID: string;
};
export type EventRow = { event_type: string; sequence: number; payload?: Record<string, unknown>; timestamp: string; origin?: string };
export type Plan = { goal: string; planner: string; steps: { step_id: string; title: string; type: string; dependencies: string[] }[] };
export type WfRow = { ID: string; Goal: string; Planner: string; Status: string; CreatedAt: string };
export type StepRow = { StepID: string; Status: string; NodeID: string; Reason: string; TaskID: string; Error: string; OutputJSON: string };
export type Report = { workflow_id: string; goal: string; planner: string; final_status: string; executed_nodes: string[]; timeline: { step_id: string; status: string; node_id: string; reason: string }[] };
export type Approval = { id: string; workflow_id: string; step_id: string; node_id: string; action: string; args_json: string; status: string };
export type Audit = { Who: string; Action: string; Result: string; NodeID: string; CreatedAt: string; Permission: string };
export type Model = { base_url: string; model: string; api_key?: string; max_steps: number; timeout_sec: number; token_budget: number; configured: boolean };
export type Capability = {
  capability_id: string; version: string; activation_status: string; verification_status: string;
  origin: string; content_hash: string; runtime: string;
  manifest: { metadata: { description: string }; spec: { platforms: string[]; permissions: { network: boolean } } };
};
export type Conversation = { id: string; title: string; updated_at: string };
export type Msg = { id: string; role: string; content: string; task_id?: string; seq: number };
export type Evolution = {
  evolution_id: string; original_goal: string; state: string; final_status: string; error?: string;
  capability_id?: string; generated_version?: string; token_usage: number;
  test_results?: Record<string, boolean>; evidence?: { phase: string; detail: string }[];
};
