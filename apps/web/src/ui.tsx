import { useEffect, useState } from "react";
import { Link, NavLink, Route, Routes, useNavigate, useParams } from "react-router-dom";
import { api, loadCSRF, setCSRF, type Approval, type NodeInfo, type TaskRow } from "./api";

function Shell({ children }: { children: React.ReactNode }) {
  return (
    <div className="app">
      <nav className="side">
        <div className="brand">Omnoda</div>
        <div className="slogan">Every Device. One Mind.</div>
        <NavLink to="/" end>Chat</NavLink>
        <NavLink to="/nodes">Nodes</NavLink>
        <NavLink to="/workflows">Workflows</NavLink>
        <NavLink to="/history">History</NavLink>
        <NavLink to="/settings">Settings</NavLink>
      </nav>
      <main>{children}</main>
      <nav className="bottom">
        <Link to="/">Chat</Link>
        <Link to="/nodes">Nodes</Link>
        <Link to="/workflows">Flows</Link>
        <Link to="/settings">Settings</Link>
      </nav>
    </div>
  );
}

export function App() {
  const [ready, setReady] = useState(false);
  const [authed, setAuthed] = useState(false);
  const [setup, setSetup] = useState(false);
  useEffect(() => {
    loadCSRF();
    api.meta().then((m) => {
      setSetup(m.setup_required);
      if (!m.setup_required) {
        api.me().then((me) => { setCSRF(me.csrf); setAuthed(true); }).catch(() => setAuthed(false)).finally(() => setReady(true));
      } else setReady(true);
    }).catch(() => setReady(true));
  }, []);
  if (!ready) return <p className="muted" style={{ padding: 24 }}>Connecting to control plane…</p>;
  if (setup) return <Setup onDone={() => { setSetup(false); }} />;
  if (!authed) return <Login onDone={() => setAuthed(true)} />;
  return (
    <Shell>
      <Routes>
        <Route path="/" element={<Chat />} />
        <Route path="/nodes" element={<Nodes />} />
        <Route path="/nodes/:id" element={<NodeDetail />} />
        <Route path="/workflows" element={<Workflows />} />
        <Route path="/workflows/:id" element={<WorkflowDetail />} />
        <Route path="/history" element={<History />} />
        <Route path="/settings" element={<Settings />} />
      </Routes>
    </Shell>
  );
}

function Setup({ onDone }: { onDone: () => void }) {
  const [username, setUsername] = useState("admin");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState("");
  return (
    <form className="auth card" onSubmit={async (e) => {
      e.preventDefault();
      try { await api.setup(username, password); onDone(); } catch (ex) { setErr(String(ex)); }
    }}>
      <h1>Initialize Omnoda</h1>
      <p className="muted">First start creates the only administrator. There is no built-in password.</p>
      <input value={username} onChange={(e) => setUsername(e.target.value)} placeholder="username" />
      <input style={{ marginTop: 8 }} type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="password (10+ chars)" />
      {err && <p className="pill failed">{err}</p>}
      <button className="primary" style={{ marginTop: 12 }} type="submit">Create admin</button>
    </form>
  );
}

function Login({ onDone }: { onDone: () => void }) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState("");
  return (
    <form className="auth card" onSubmit={async (e) => {
      e.preventDefault();
      try { await api.login(username, password); onDone(); } catch (ex) { setErr((ex as Error).message); }
    }}>
      <h1>Omnoda</h1>
      <p className="muted">Every Device. One Mind.</p>
      <input value={username} onChange={(e) => setUsername(e.target.value)} placeholder="username" />
      <input style={{ marginTop: 8 }} type="password" value={password} onChange={(e) => setPassword(e.target.value)} placeholder="password" />
      {err && <p>{err}</p>}
      <button className="primary" style={{ marginTop: 12 }} type="submit">Sign in</button>
    </form>
  );
}

function Chat() {
  const [goal, setGoal] = useState("检查 ARM 服务器上的 Demo 服务。如果异常，让 Mac 上的 Claude Code 修复代码，然后到 Windows 工作站运行测试。");
  const [planner, setPlanner] = useState("template");
  const [nodes, setNodes] = useState<NodeInfo[]>([]);
  const [nodeID, setNodeID] = useState("");
  const [harness, setHarness] = useState("auto");
  const [prompt, setPrompt] = useState("");
  const [msg, setMsg] = useState("");
  const nav = useNavigate();
  useEffect(() => { api.nodes().then((r) => setNodes(r.nodes || [])).catch((e) => setMsg(e.message)); }, []);
  return (
    <div>
      <h2>Chat</h2>
      <p className="muted">Template planner runs the known diagnose → fix → test workflow. LLM planner is labeled separately and only runs when a model is configured.</p>
      <div className="card">
        <label className="muted">Goal</label>
        <textarea rows={4} value={goal} onChange={(e) => setGoal(e.target.value)} />
        <div className="row" style={{ marginTop: 8 }}>
          <select value={planner} onChange={(e) => setPlanner(e.target.value)} style={{ maxWidth: 220 }}>
            <option value="template">Template planner</option>
            <option value="llm">LLM planner</option>
            <option value="auto">Auto (LLM if configured)</option>
          </select>
          <button className="primary" onClick={async () => {
            setMsg("Planning…");
            try {
              const r = await api.workflow(goal, planner);
              setMsg(`Started ${r.workflow_id} with ${r.planner} planner`);
              nav(`/workflows/${r.workflow_id}`);
            } catch (e) { setMsg((e as Error).message); }
          }}>Run workflow</button>
        </div>
      </div>
      <div className="card">
        <h3>Direct node task</h3>
        {nodes.length === 0 && <p className="muted">No nodes yet. Add one from Nodes. This list is empty because the registry is empty.</p>}
        <div className="row">
          <select value={nodeID} onChange={(e) => setNodeID(e.target.value)}>
            <option value="">Select node</option>
            {nodes.map((n) => <option key={n.id} value={n.id}>{n.display_name} ({n.status})</option>)}
          </select>
          <select value={harness} onChange={(e) => setHarness(e.target.value)} style={{ maxWidth: 200 }}>
            <option value="auto">Command</option>
            <option value="codex">Codex</option>
            <option value="claude-code">Claude Code</option>
            <option value="opencode">OpenCode</option>
            <option value="native">Native agent</option>
          </select>
        </div>
        <textarea style={{ marginTop: 8 }} rows={3} placeholder={harness === "auto" ? "command, for example: uname -a" : "prompt"} value={prompt} onChange={(e) => setPrompt(e.target.value)} />
        <button style={{ marginTop: 8 }} onClick={async () => {
          if (!nodeID) { setMsg("Select a node"); return; }
          try {
            const body: Record<string, unknown> = { node_id: nodeID, timeout_sec: 120 };
            if (harness === "auto") {
              const parts = prompt.trim().split(/\s+/);
              body.kind = "command"; body.command = parts[0]; body.args = parts.slice(1); body.level = "READ_ONLY";
            } else if (harness === "native") {
              body.kind = "native"; body.prompt = prompt; body.level = "READ_ONLY";
            } else {
              body.kind = "harness"; body.harness_id = harness; body.prompt = prompt; body.level = "WORKSPACE_WRITE";
            }
            const r = await api.task(body);
            nav(`/history?task=${r.task_id}`);
          } catch (e) { setMsg((e as Error).message); }
        }}>Send</button>
      </div>
      {msg && <p>{msg}</p>}
      <Approvals />
    </div>
  );
}

function Approvals() {
  const [rows, setRows] = useState<Approval[]>([]);
  const load = () => api.approvals().then((r) => setRows(r.approvals || [])).catch(() => {});
  useEffect(() => { load(); const t = setInterval(load, 2000); return () => clearInterval(t); }, []);
  if (!rows.length) return null;
  return (
    <div className="card">
      <h3>Approvals</h3>
      {rows.map((a) => (
        <div key={a.id} className="row" style={{ marginBottom: 8 }}>
          <span>{a.action} on {a.node_id || "node"} · step {a.step_id}</span>
          <button className="primary" onClick={() => api.decide(a.id, true).then(load)}>Approve</button>
          <button className="danger" onClick={() => api.decide(a.id, false).then(load)}>Reject</button>
        </div>
      ))}
    </div>
  );
}

function Nodes() {
  const [nodes, setNodes] = useState<NodeInfo[]>([]);
  const [name, setName] = useState("");
  const [os, setOS] = useState("linux");
  const [cmd, setCmd] = useState<{ sh: string; ps: string; exp: string } | null>(null);
  const [err, setErr] = useState("");
  const load = () => api.nodes().then((r) => setNodes(r.nodes || [])).catch((e) => setErr(e.message));
  useEffect(() => { load(); const t = setInterval(load, 3000); return () => clearInterval(t); }, []);
  return (
    <div>
      <h2>Nodes</h2>
      {err && <p>{err}</p>}
      <div className="card">
        <h3>Add node</h3>
        <div className="row">
          <input placeholder="display name" value={name} onChange={(e) => setName(e.target.value)} />
          <select value={os} onChange={(e) => setOS(e.target.value)} style={{ maxWidth: 180 }}>
            <option value="linux">Linux</option>
            <option value="darwin">macOS</option>
            <option value="windows">Windows</option>
          </select>
          <button className="primary" onClick={async () => {
            const r = await api.enroll(name || "node", os);
            setCmd({ sh: r.install_sh, ps: r.install_ps, exp: r.expires_at });
          }}>Create enrollment code</button>
        </div>
        {cmd && (
          <div>
            <p className="muted">Expires {cmd.exp}. The code works once.</p>
            <pre>{cmd.sh}</pre>
            <pre>{cmd.ps}</pre>
          </div>
        )}
      </div>
      {nodes.length === 0 && <div className="card"><p>No devices enrolled. Create a code and run it on the machine you want to add.</p></div>}
      {nodes.map((n) => (
        <div className="card" key={n.id}>
          <div className="row">
            <Link to={`/nodes/${n.id}`}><strong>{n.display_name}</strong></Link>
            <span className={`pill ${n.status}`}>{n.status}</span>
            <span className="muted">{n.os}/{n.arch}</span>
            <span className="muted">{n.version}</span>
          </div>
          <p className="muted">{(n.harnesses || []).filter((h) => h.present).map((h) => h.id + " " + (h.version || "")).join(" · ") || "no harness detected"}</p>
          <p className="muted">labels: {Object.entries(n.labels || {}).map(([k, v]) => `${k}=${v}`).join(" ") || "none"}</p>
        </div>
      ))}
    </div>
  );
}

function NodeDetail() {
  const { id } = useParams();
  const [n, setN] = useState<NodeInfo | null>(null);
  const [cmd, setCmd] = useState("uname -a");
  const [out, setOut] = useState("");
  const nav = useNavigate();
  useEffect(() => { if (id) api.node(id).then(setN).catch((e) => setOut(e.message)); }, [id]);
  if (!n) return <p>Loading…</p>;
  return (
    <div>
      <h2>{n.display_name}</h2>
      <p><span className={`pill ${n.status}`}>{n.status}</span> {n.os}/{n.arch} · {n.hostname} · last seen {n.last_seen || "never"}</p>
      <div className="card">
        <h3>Harnesses</h3>
        {(n.harnesses || []).map((h) => <p key={h.id}>{h.id}: {h.present ? `${h.version} @ ${h.path}` : h.error}</p>)}
        <h3>Workspaces</h3>
        {(n.workspaces || []).map((w) => <p key={w}>{w}</p>)}
        {(n.workspaces || []).length === 0 && <p className="muted">No authorized workspace.</p>}
      </div>
      <div className="card">
        <h3>Read-only command</h3>
        <input value={cmd} onChange={(e) => setCmd(e.target.value)} />
        <button style={{ marginTop: 8 }} onClick={async () => {
          const parts = cmd.trim().split(/\s+/);
          try {
            const r = await api.task({ node_id: n.id, kind: "command", command: parts[0], args: parts.slice(1), level: "READ_ONLY", timeout_sec: 30 });
            setOut("task " + r.task_id);
            nav(`/history?task=${r.task_id}`);
          } catch (e) { setOut((e as Error).message); }
        }}>Run</button>
        {out && <p>{out}</p>}
      </div>
      <button className="danger" onClick={async () => { await api.revoke(n.id); nav("/nodes"); }}>Revoke device</button>
    </div>
  );
}

function Workflows() {
  const [rows, setRows] = useState<{ ID: string; Goal: string; Planner: string; Status: string }[]>([]);
  useEffect(() => { api.workflows().then((r) => setRows(r.workflows || [])); }, []);
  return (
    <div>
      <h2>Workflows</h2>
      {rows.length === 0 && <p className="muted">No workflows yet.</p>}
      {rows.map((w) => (
        <div className="card" key={w.ID}>
          <Link to={`/workflows/${w.ID}`}>{w.Goal}</Link>
          <div className="row"><span className={`pill ${w.Status}`}>{w.Status}</span><span className="muted">{w.Planner} planner</span></div>
        </div>
      ))}
    </div>
  );
}

function WorkflowDetail() {
  const { id } = useParams();
  const [data, setData] = useState<Awaited<ReturnType<typeof api.workflowDetail>> | null>(null);
  useEffect(() => {
    if (!id) return;
    const load = () => api.workflowDetail(id).then(setData).catch(() => {});
    load();
    const t = setInterval(load, 1500);
    return () => clearInterval(t);
  }, [id]);
  if (!data) return <p>Loading…</p>;
  const steps = data.steps || [];
  return (
    <div>
      <h2>Workflow</h2>
      <p>{data.workflow.Goal}</p>
      <p><span className={`pill ${data.workflow.Status}`}>{data.workflow.Status}</span> <strong>{data.workflow.Planner} planner</strong></p>
      <div className="dag">
        {steps.map((s) => (
          <div className="card step" key={s.StepID}>
            <strong>{s.StepID}</strong>
            <div><span className={`pill ${s.Status}`}>{s.Status}</span></div>
            <p className="muted">{s.NodeID}</p>
            <p className="muted">{s.Reason}</p>
            {s.Error && <p>{s.Error}</p>}
            {s.TaskID && <Link to={`/history?task=${s.TaskID}`}>logs</Link>}
          </div>
        ))}
      </div>
      <Approvals />
      <button onClick={() => id && api.cancelWorkflow(id)}>Cancel</button>
      {data.report && (
        <div className="card">
          <h3>Report</h3>
          <pre>{JSON.stringify(data.report, null, 2)}</pre>
        </div>
      )}
    </div>
  );
}

function History() {
  const params = new URLSearchParams(location.search);
  const focus = params.get("task");
  const [tasks, setTasks] = useState<TaskRow[]>([]);
  const [events, setEvents] = useState<string>("");
  const [detail, setDetail] = useState<TaskRow | null>(null);
  useEffect(() => {
    api.tasks().then((r) => setTasks(r.tasks || []));
  }, []);
  useEffect(() => {
    if (!focus) return;
    const load = () => {
      api.taskDetail(focus).then((r) => setDetail(r.task)).catch(() => {});
      api.events(focus).then((r) => {
        setEvents((r.events || []).map((e) => `${e.event_type} ${JSON.stringify(e.payload || {})}`).join("\n"));
      }).catch(() => {});
    };
    load();
    const t = setInterval(load, 1000);
    return () => clearInterval(t);
  }, [focus]);
  return (
    <div>
      <h2>History</h2>
      {focus && (
        <div className="card">
          <div className="row">
            <strong>{focus}</strong>
            {detail && <span className={`pill ${detail.Status}`}>{detail.Status}</span>}
            <button onClick={() => api.cancelTask(focus)}>Cancel</button>
          </div>
          {detail && <pre>{detail.Output || detail.Summary || detail.Error}</pre>}
          <div className="log">{events || "waiting for events…"}</div>
        </div>
      )}
      {tasks.map((t) => (
        <div key={t.ID} className="card">
          <Link to={`/history?task=${t.ID}`}>{t.ID}</Link> <span className={`pill ${t.Status}`}>{t.Status}</span>
          <span className="muted"> {t.Kind} {t.NodeID}</span>
          <p>{t.Summary || t.Error}</p>
        </div>
      ))}
    </div>
  );
}

function Settings() {
  const [model, setModel] = useState({ base_url: "", model: "", api_key: "", max_steps: 8, timeout_sec: 120, token_budget: 8000, configured: false });
  const [audit, setAudit] = useState<{ Action: string; Result: string; CreatedAt: string; Who: string }[]>([]);
  const [theme, setTheme] = useState(localStorage.getItem("omnoda_theme") || "dark");
  useEffect(() => {
    api.model().then((m) => setModel({ ...model, ...m, api_key: "" })).catch(() => {});
    api.audit().then((a) => setAudit(a.audit || [])).catch(() => {});
  }, []);
  return (
    <div>
      <h2>Settings</h2>
      <div className="card">
        <h3>Appearance</h3>
        <button onClick={() => {
          const next = theme === "light" ? "dark" : "light";
          setTheme(next);
          localStorage.setItem("omnoda_theme", next);
          document.documentElement.dataset.theme = next === "light" ? "light" : "";
        }}>Theme: {theme}</button>
      </div>
      <div className="card">
        <h3>Model provider (OpenAI-compatible)</h3>
        <p className="muted">The API key is sealed locally and never returned. Configured: {String(model.configured)}</p>
        <input placeholder="base URL" value={model.base_url} onChange={(e) => setModel({ ...model, base_url: e.target.value })} />
        <input style={{ marginTop: 8 }} placeholder="model" value={model.model} onChange={(e) => setModel({ ...model, model: e.target.value })} />
        <input style={{ marginTop: 8 }} placeholder="API key" type="password" value={model.api_key} onChange={(e) => setModel({ ...model, api_key: e.target.value })} />
        <button className="primary" style={{ marginTop: 8 }} onClick={() => api.saveModel(model).then(() => setModel({ ...model, configured: true, api_key: "" }))}>Save</button>
      </div>
      <div className="card">
        <h3>Audit</h3>
        {audit.slice(0, 30).map((a, i) => <p key={i} className="muted">{a.CreatedAt} {a.Who} {a.Action} {a.Result}</p>)}
      </div>
      <button onClick={() => api.logout().then(() => location.reload())}>Sign out</button>
    </div>
  );
}
