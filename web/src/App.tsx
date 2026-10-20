import { Attachments } from "./Attachments";
import { eventMessage, useLiveConversation } from "./live";
import {
  AnswerSuggestions,
  PublishAnswerForm,
  SharedAnswers,
  FleetSummary,
} from "./Assistance";
import {
  useCallback,
  memo,
  useEffect,
  useRef,
  useState,
  type FormEvent,
  type ReactNode,
} from "react";
import {
  api,
  APIError,
  timeAgo,
  requestLabel,
  questionGroupKey,
  type Entity,
  type RequestItem,
  type Conversation,
  type Group,
  type Role,
  type Connection,
  type EventItem,
  type Instance,
  type Me,
  type Target,
  type Rule,
  type Message,
} from "./api";

const nav = [
  ["inbox", "Inbox", "inbox"],
  ["conversations", "Conversations", "chat"],
  ["agents", "Agents", "agent"],
  ["people", "People", "people"],
  ["groups", "Groups", "group"],
  ["permissions", "Permissions", "key"],
  ["connections", "Connections", "plug"],
  ["activity", "Activity", "pulse"],
  ["settings", "Settings", "settings"],
] as const;
type Page = (typeof nav)[number][0];
type Data = {
  people: Entity[];
  agents: Entity[];
  services: Entity[];
  requests: RequestItem[];
  conversations: Conversation[];
  groups: Group[];
  roles: Role[];
  connections: Connection[];
  events: EventItem[];
  destinations: {
    id: string;
    connection_id: string;
    principal_id: string;
    address: string;
    mode: string;
    priority: number;
  }[];
  bindings: { role_id: string; subject_kind: string; subject_id: string }[];
};
const emptyData: Data = {
  people: [],
  agents: [],
  services: [],
  requests: [],
  conversations: [],
  groups: [],
  roles: [],
  connections: [],
  events: [],
  destinations: [],
  bindings: [],
};
function Icon({ name, size = 18 }: { name: string; size?: number }) {
  const paths: Record<string, ReactNode> = {
    inbox: (
      <>
        <path d="M4 4h16v16H4zM4 13h4l2 3h4l2-3h4" />
      </>
    ),
    chat: (
      <>
        <path d="M4 4h16v13H9l-5 4z" />
        <path d="M8 8h8M8 12h5" />
      </>
    ),
    agent: (
      <>
        <rect x="4" y="6" width="16" height="14" rx="4" />
        <path d="M12 2v4M8 12h.01M16 12h.01M9 16h6" />
      </>
    ),
    people: (
      <>
        <circle cx="9" cy="8" r="3" />
        <path d="M3 20v-2a6 6 0 0 1 12 0v2M16 5a3 3 0 0 1 0 6M18 15a5 5 0 0 1 3 5" />
      </>
    ),
    group: (
      <>
        <circle cx="12" cy="8" r="3" />
        <path d="M6 21v-3a6 6 0 0 1 12 0v3M3 5v6M0 8h6" />
      </>
    ),
    key: (
      <>
        <circle cx="8" cy="9" r="5" />
        <path d="m12 13 8 8M16 17l3-3M18 19l3-3" />
      </>
    ),
    plug: (
      <>
        <path d="m8 3 3 3M15 2l3 3M5 9l7-7 8 8-7 7H8L5 14zM8 17l-5 5" />
      </>
    ),
    pulse: (
      <>
        <path d="M2 12h5l3-8 4 16 3-8h5" />
      </>
    ),
    settings: (
      <>
        <circle cx="12" cy="12" r="4" />
        <path d="M12 2v3M12 19v3M2 12h3M19 12h3M5 5l2 2M17 17l2 2M5 19l2-2M17 7l2-2" />
      </>
    ),
    arrow: <path d="m8 5 7 7-7 7" />,
    plus: <path d="M12 4v16M4 12h16" />,
    check: <path d="m4 12 5 5L20 6" />,
    close: <path d="m6 6 12 12M6 18 18 6" />,
    send: <path d="m3 3 19 9-19 9 4-9-4-9zM7 12h15" />,
    search: (
      <>
        <circle cx="10" cy="10" r="6" />
        <path d="m15 15 6 6" />
      </>
    ),
    logout: (
      <>
        <path d="M10 3H4v18h6M14 8l5 4-5 4M8 12h11" />
      </>
    ),
  };
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.6"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {paths[name] ?? paths.chat}
    </svg>
  );
}
function Badge({
  children,
  tone = "muted",
}: {
  children: ReactNode;
  tone?: string;
}) {
  return <span className={"badge " + tone}>{children}</span>;
}
function Avatar({ person }: { person?: Entity }) {
  return (
    <span className={"avatar " + (person?.kind === "agent" ? "robot" : "")}>
      {person?.kind === "agent" ? (
        <Icon name="agent" size={20} />
      ) : (
        (person?.name?.slice(0, 2).toUpperCase() ?? "?")
      )}
    </span>
  );
}
function Empty({
  icon,
  title,
  children,
  action,
}: {
  icon: string;
  title: string;
  children: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="empty">
      <div className="empty-icon">
        <Icon name={icon} size={28} />
      </div>
      <h2>{title}</h2>
      <p>{children}</p>
      {action}
    </div>
  );
}
function Modal({
  title,
  children,
  onClose,
}: {
  title: string;
  children: ReactNode;
  onClose: () => void;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    ref.current?.showModal();
  }, []);
  return (
    <dialog
      ref={ref}
      onCancel={onClose}
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="modal-head">
        <h2>{title}</h2>
        <button
          className="icon-button"
          onClick={onClose}
          aria-label="Close dialog"
        >
          <Icon name="close" />
        </button>
      </div>
      {children}
    </dialog>
  );
}
function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="field">
      <span>{label}</span>
      {children}
    </label>
  );
}
function Login({ onLogin }: { onLogin: () => void }) {
  const [token, setToken] = useState(""),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [oidc, setOIDC] = useState(false);
  useEffect(() => {
    api<{ oidc: boolean }>("/auth/methods")
      .then((x) => setOIDC(x.oidc))
      .catch(() => {});
  }, []);
  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      await api("/auth/login", { token });
      setToken("");
      onLogin();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <main className="login">
      <div className="login-art">
        <div className="brand large">
          <span className="brand-mark">n</span>notif
          <span className="brand-period">.</span>
        </div>
        <h1>
          Every agent.
          <br />A clear connection.
        </h1>
        <p>
          See who is working. Reach the right person.
          <br />
          Keep decisions moving.
        </p>
        <div className="connection-illustration">
          <span className="node human">You</span>
          <i />
          <span className="node hub">n</span>
          <i />
          <div>
            <span className="node worker">
              Scout <b />
            </span>
            <span className="node worker">
              Builder <b />
            </span>
          </div>
        </div>
        <span className="login-foot">
          Open source. Runs on your infrastructure.
        </span>
      </div>
      <form className="login-form" onSubmit={submit}>
        <span className="eyebrow">YOUR WORKSPACE</span>
        <h2>Welcome to Notif</h2>
        <p>Sign in with your personal access token.</p>
        <Field label="Access token">
          <input
            type="password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder="Paste your access token"
            autoComplete="off"
            required
            autoFocus
          />
        </Field>
        {error && (
          <p role="alert" className="error">
            {error}
          </p>
        )}
        <button className="primary wide" disabled={busy}>
          {busy ? "Signing in…" : "Open workspace"}
          <Icon name="arrow" />
        </button>
        {oidc && (
          <a className="secondary wide sso-button" href="/auth/oidc/start">
            Continue with company sign-in
          </a>
        )}
        <div className="setup-note">
          <strong>Running locally?</strong>
          <p>
            Your owner token is in <code>.local/owner-token</code> inside the
            Notif project. Other people can sign in with their own issued token.
          </p>
        </div>
      </form>
    </main>
  );
}

export function App() {
  const [me, setMe] = useState<Me | null>(null),
    [checking, setChecking] = useState(true),
    [data, setData] = useState<Data>(emptyData),
    [page, setPage] = useState<Page>("inbox"),
    [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [modal, setModal] = useState<string | null>(null),
    [selected, setSelected] = useState<string | null>(null),
    [query, setQuery] = useState(""),
    [snapshotRevision, setSnapshotRevision] = useState(0),
    [attachmentRevision, setAttachmentRevision] = useState(0),
    [instances, setInstances] = useState<Instance[]>([]),
    [tab, setTab] = useState("open"),
    [live, setLive] = useState("Connecting");
  const meRef = useRef(me);
  meRef.current = me;
  const dataRef = useRef(data);
  dataRef.current = data;
  const pageRef = useRef(page);
  pageRef.current = page;
  const activeConversation = page === "conversations" ? selected : null;
  const { conversation, setConversation, acceptMessage } = useLiveConversation(me ? activeConversation : null, snapshotRevision, setError);
  const refreshGeneration = useRef(0);
  const refresh = useCallback(async () => {
    const generation = ++refreshGeneration.current;
    try {
      const current = await api<Me>("/me");
      const entries: [keyof Data, string][] = [
        ["people", "/people"],
        ["agents", "/agents"],
        ["services", "/services"],
        ["requests", "/requests"],
        ["conversations", "/conversations"],
        ["groups", "/groups"],
        ["events", "/activity"],
      ];
      if (current.admin)
        entries.push(
          ["roles", "/roles"],
          ["connections", "/connections"],
          ["bindings", "/role-bindings"],
          ["destinations", "/destinations"],
        );
      const values = await Promise.all(
        entries.map(async ([k, p]) => [k, await api(p)]),
      );
      if (generation !== refreshGeneration.current) return;
      setMe(current);
      setData({ ...emptyData, ...Object.fromEntries(values) });
      setSnapshotRevision(revision => revision + 1);
      setError("");
    } catch (e) {
      if (generation !== refreshGeneration.current) return;
      setData(emptyData);
      setConversation(null);
      setInstances([]);
      setSnapshotRevision(revision => revision + 1);
      if (e instanceof APIError && e.status === 401) setMe(null);
      else setError((e as Error).message);
    } finally {
      if (generation === refreshGeneration.current) setChecking(false);
    }
  }, []);
  useEffect(() => {
    void refresh();
  }, [refresh]);
  const reconcileTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const scheduleRefresh = useCallback(() => {
    if (!reconcileTimer.current)
      reconcileTimer.current = setTimeout(() => {
        reconcileTimer.current = undefined;
        void refresh();
      }, 2000);
  }, [refresh]);
  const streamCursor = useRef({ identity: "", sequence: 0 });
  const streamIdentity = me ? JSON.stringify([me.company, me.environment, me.person.id]) : "";
  useEffect(() => () => {
    clearTimeout(reconcileTimer.current);
    reconcileTimer.current = undefined;
  }, [streamIdentity]);
  useEffect(() => {
    if (!me) return;
    let active = true;
    if (streamCursor.current.identity !== streamIdentity)
      streamCursor.current = { identity: streamIdentity, sequence: Math.max(0, ...dataRef.current.events.map(e => e.sequence)) };
    const query = new URLSearchParams({ stream: "1", summary: "1", after: String(streamCursor.current.sequence) });
    if (activeConversation) query.set("conversation_id", activeConversation);
    const source = new EventSource("/api/v1/events?" + query);
    source.onopen = () => { if (active) setLive("Live updates"); };
    source.onerror = () => { if (active) setLive("Reconnecting"); };
    const reconcile = scheduleRefresh;
    source.addEventListener("cursor", (event) => {
      const sequence = Number((event as MessageEvent).lastEventId);
      if (active && Number.isSafeInteger(sequence))
        streamCursor.current.sequence = Math.max(streamCursor.current.sequence, sequence);
    });
    source.onmessage = (frame) => {
      if (!active) return;
      let event: EventItem;
      try { event = JSON.parse(frame.data); }
      catch { reconcile(); return; }
      if (!Number.isSafeInteger(event.sequence) || typeof event.kind !== "string" ||
          (frame.lastEventId && Number(frame.lastEventId) !== event.sequence)) { reconcile(); return; }
      if (event.sequence <= streamCursor.current.sequence) return;
      streamCursor.current.sequence = event.sequence;
      if (pageRef.current === "activity") {
        // Activity and delivery recovery retain authoritative snapshots. They
        // have no growing-chat refetch problem to justify a second live cache.
        reconcile();
      }
      if (event.kind === "message.created") {
        if (event.conversation_id === activeConversation) {
          const message = eventMessage(event);
          if (message) acceptMessage(message);
          else reconcile(); // Compatible fallback for older summary-only servers.
        }
        if (!dataRef.current.conversations.some(c => c.id === event.conversation_id)) reconcile();
      } else if (event.kind === "attachment.created") {
        if (event.conversation_id === activeConversation) setAttachmentRevision(revision => revision + 1);
      } else if (!["worker.received", "delivery.retried", "execution.authorized", "execution.reported"].includes(event.kind)) {
        // Request, directory and policy events still reconcile authoritative
        // snapshots. A message or delivery receipt cannot alter those resources.
        reconcile();
      }
    };
    return () => {
      active = false;
      source.close();
    };
  }, [streamIdentity, activeConversation, scheduleRefresh, acceptMessage]);
  useEffect(() => {
    if (!streamIdentity) return;
    // Navigation must not postpone the existing revocation/presence safety net.
    const timer = setInterval(() => void refresh(), 15000);
    return () => clearInterval(timer);
  }, [streamIdentity, refresh]);
  useEffect(() => {
    const route = () => {
      const parts = location.pathname.split("/").filter(Boolean);
      if (parts[0] === "requests" && parts[1]) {
        setPage("inbox");
        setSelected(parts[1]);
      } else {
        const found = nav.find((x) => x[0] === parts[0]);
        if (found) {
          setPage(found[0]);
          setSelected(parts[1] ?? null);
        }
      }
    };
    route();
    addEventListener("popstate", route);
    return () => removeEventListener("popstate", route);
  }, []);
  useEffect(() => {
    document.title = `${nav.find((x) => x[0] === page)?.[1] ?? "Notif"} · Notif`;
    setQuery("");
  }, [page]);
  useEffect(() => {
    if (page === "activity" && meRef.current) void refresh();
  }, [page, refresh]);
  useEffect(() => {
    let active = true;
    setInstances([]);
    if (page === "agents" && selected)
      api<Instance[]>("/agents/" + selected + "/instances")
        .then((value)=>{if(active)setInstances(value);})
        .catch((e) => {if(active){setInstances([]);setError(e.message);}});
    return ()=>{active=false;};
  }, [page, selected, snapshotRevision]);
  const entities = [...data.people, ...data.agents, ...data.services];
  const who = (id?: string) => entities.find((p) => p.id === id);
  const name = (id?: string) =>
    who(id)?.name ?? (id ? id.slice(0, 12) : "Unknown");
  async function act(task: () => Promise<unknown>, message = "Saved", reconcile = true) {
    try {
      await task();
      setNotice(message);
      setTimeout(() => setNotice(""), 3500);
      if (reconcile) await refresh();
      return true;
    } catch (e) {
      await refresh();
      setError((e as Error).message);
      return false;
    }
  }
  function navigate(next: Page) {
    setPage(next);
    setSelected(null);
    setTab("open");
    history.pushState(null, "", "/" + next);
  }
  function openConversation(id: string) {
    setPage("conversations");
    setSelected(id);
    history.pushState(null, "", "/conversations/" + id);
  }
  if (checking)
    return (
      <div className="loading-screen">
        <span className="brand-mark">n</span>
        <p>Opening your workspace…</p>
      </div>
    );
  if (!me) return <Login onLogin={() => void refresh()} />;
  const filtered = (values: Entity[]) =>
    values.filter((p) =>
      (
        p.name +
        " " +
        p.external_id +
        " " +
        String(p.metadata.department ?? "") +
        " " +
        String(p.metadata.project ?? "")
      )
        .toLowerCase()
        .includes(query.toLowerCase()),
    );
  const open = data.requests.filter((r) => r.state === "open"),
    current = data.requests.find((r) => r.id === selected),
    agent = data.agents.find((a) => a.id === selected);
  const bundles = new Map<string, RequestItem[]>();
  for (const r of open) {
    const key = questionGroupKey(r);
    bundles.set(key, [...(bundles.get(key) ?? []), r]);
  }
  const attention = [...bundles.values()].map((group) => group[0]);
  const matching =
    current?.state === "open"
      ? (bundles.get(questionGroupKey(current)) ?? [current])
      : [];
  const subtitle: Record<Page, string> = {
    inbox: "Questions and decisions that need a human.",
    conversations: "Keep the context. Continue the conversation.",
    agents: "Connect once. See every running instance.",
    people: "Your team, separate from your agents.",
    groups: "Organize people and agents into explicit audiences.",
    permissions: "Control who may contact, request work, and decide.",
    connections: "Reach people where they already work.",
    activity: "Follow each message, decision and delivery.",
    settings: "Manage access to your self-hosted workspace.",
  };
  return (
    <div className="shell">
      <aside className="sidebar">
        <a
          href="/inbox"
          className="brand"
          onClick={(e) => {
            e.preventDefault();
            navigate("inbox");
          }}
        >
          <span className="brand-mark">n</span>notif
          <span className="brand-period">.</span>
        </a>
        <div className="workspace">
          <span className="workspace-symbol">{me.company.slice(0, 1)}</span>
          <span>
            <strong>{me.company}</strong>
            <small>{me.environment}</small>
          </span>
          <span className="workspace-chevron">⌄</span>
        </div>
        <div className="nav-label">WORKSPACE</div>
        <nav aria-label="Main navigation">
          {nav
            .filter(
              (x) =>
                me.admin ||
                !["permissions", "connections", "settings"].includes(x[0]),
            )
            .map(([key, label, icon]) => (
              <button
                key={key}
                aria-label={label}
                className={page === key ? "nav-item active" : "nav-item"}
                onClick={() => navigate(key)}
              >
                <Icon name={icon} />
                <span>{label}</span>
                {key === "inbox" && open.length > 0 && (
                  <b className="nav-count">{open.length}</b>
                )}
              </button>
            ))}
        </nav>
        <div className="sidebar-bottom">
          <span className="self-hosted">
            <i />
            Self-hosted
          </span>
          <div className="profile">
            <Avatar person={me.person} />
            <span>
              <strong>{me.person.name}</strong>
              <small>{me.admin ? "Workspace owner" : "Team member"}</small>
            </span>
            <button
              className="icon-button"
              aria-label="Sign out"
              onClick={() =>
                void act(async () => {
                  await api("/auth/logout", {});
                  setMe(null);
                }, "Signed out")
              }
            >
              <Icon name="logout" />
            </button>
          </div>
        </div>
      </aside>
      <main className="main">
        <div className="topbar">
          <span>
            Workspace <span className="slash">/</span>{" "}
            <strong>{nav.find((x) => x[0] === page)?.[1]}</strong>
          </span>
          <div>
            <span
              className={
                "live-dot " + (live !== "Live updates" ? "reconnecting" : "")
              }
            />
            {live} <span className="environment">{me.environment}</span>
          </div>
        </div>
        <div className="content">
          <header className="page-header">
            <div>
              <div className="eyebrow">
                {page === "inbox" ? "YOUR ATTENTION, WELL SPENT" : "WORKSPACE"}
              </div>
              <h1>{nav.find((x) => x[0] === page)?.[1]}</h1>
              <p>{subtitle[page]}</p>
            </div>
            <div className="header-actions">
              {page === "agents" && (
                <button
                  className="primary"
                  onClick={() => setModal("connect-agent")}
                >
                  <Icon name="plus" />
                  Connect an agent
                </button>
              )}
              {page === "people" && me.admin && (
                <button className="primary" onClick={() => setModal("person")}>
                  <Icon name="plus" />
                  Add person
                </button>
              )}
              {page === "groups" && me.admin && (
                <button className="primary" onClick={() => setModal("group")}>
                  <Icon name="plus" />
                  Create group
                </button>
              )}
              {page === "permissions" && me.admin && (
                <button className="primary" onClick={() => setModal("role")}>
                  <Icon name="plus" />
                  Create role
                </button>
              )}
              {page === "connections" && me.admin && (
                <button
                  className="primary"
                  onClick={() => setModal("connection")}
                >
                  <Icon name="plus" />
                  Add connection
                </button>
              )}
              {page === "conversations" && (
                <button className="primary" onClick={() => setModal("compose")}>
                  <Icon name="plus" />
                  New message
                </button>
              )}
            </div>
          </header>
          {error && (
            <div role="alert" className="error-banner">
              <span>{error}</span>
              <button
                className="icon-button"
                aria-label="Dismiss error"
                onClick={() => setError("")}
              >
                <Icon name="close" />
              </button>
            </div>
          )}
          {notice && (
            <div role="status" className="toast">
              <Icon name="check" />
              {notice}
            </div>
          )}
          {page === "inbox" && (
            <>
              <div className="section-toolbar">
                <div className="tabs">
                  <button
                    className={tab === "open" ? "selected" : ""}
                    onClick={() => {
                      setTab("open");
                      setSelected(null);
                    }}
                  >
                    Needs you <span>{open.length}</span>
                  </button>
                  <button
                    className={tab === "all" ? "selected" : ""}
                    onClick={() => {
                      setTab("all");
                      setSelected(null);
                    }}
                  >
                    All requests
                  </button>
                </div>
                <button className="quiet" onClick={() => setModal("ask")}>
                  Ask a question <Icon name="plus" size={15} />
                </button>
              </div>
              {data.requests.length === 0 ? (
                <Empty icon="inbox" title="You're all caught up">
                  Questions from your agents will appear here. Connect an agent
                  or ask your first question.
                </Empty>
              ) : (
                <div
                  className={"split-workspace " + (current ? "has-detail" : "")}
                >
                  <div className="request-list">
                    {(tab === "open" ? attention : data.requests).map((r) => (
                      <button
                        key={r.id}
                        className={
                          "request-row " + (selected === r.id ? "selected" : "")
                        }
                        onClick={() => setSelected(r.id)}
                      >
                        <Avatar person={who(r.requester_id)} />
                        <div className="row-main">
                          <div className="row-meta">
                            <strong>{name(r.requester_id)}</strong>
                            <span>{timeAgo(r.created_at)}</span>
                          </div>
                          <h3>{r.title}</h3>
                          {(bundles.get(questionGroupKey(r))?.length ?? 0) >
                            1 && (
                            <Badge>
                              {bundles.get(questionGroupKey(r))!.length}{" "}
                              matching questions
                            </Badge>
                          )}
                          <div className="row-bottom">
                            <Badge
                              tone={r.kind === "approval" ? "amber" : "muted"}
                            >
                              {r.kind}
                            </Badge>
                            <span>
                              {r.body.slice(0, 85) ||
                                "Open to review and respond"}
                            </span>
                          </div>
                        </div>
                        <Badge tone={r.state === "open" ? "green" : "muted"}>
                          {requestLabel(r)}
                        </Badge>
                        <Icon name="arrow" size={16} />
                      </button>
                    ))}
                    {tab === "open" && open.length === 0 && (
                      <Empty icon="check" title="No pending requests">
                        Every current request has a recorded outcome.
                      </Empty>
                    )}
                  </div>
                  {current ? (
                    <RequestDetail
                      request={current}
                      sender={who(current.requester_id)}
                      me={me}
                      position={open.findIndex((r) => r.id === current.id) + 1}
                      total={open.length}
                      matchingCount={matching.length}
                      matchingNames={[
                        ...new Set(matching.map((r) => name(r.requester_id))),
                      ]}
                      onClose={() => setSelected(null)}
                      onConversation={() =>
                        openConversation(current.conversation_id)
                      }
                      onRespond={(outcome, answer, share) =>
                        act(async () => {
                          await api(
                            "/requests/" +
                              current.id +
                              (share ? "/answer-and-share" : "/responses"),
                            {
                              outcome,
                              answer,
                              version: current.version,
                              ...(share
                                ? {
                                    audience: [
                                      ...new Set(
                                        matching.map((r) => r.requester_id),
                                      ),
                                    ],
                                  }
                                : {}),
                            },
                          );
                          const next = open.find(
                            (r) =>
                              r.id !== current.id &&
                              (!share || !matching.some((m) => m.id === r.id)),
                          );
                          setSelected(next?.id ?? current.id);
                        }, "Response recorded")
                      }
                      onEnd={(state) =>
                        act(
                          () =>
                            api("/requests/" + current.id + "/" + state, {
                              version: current.version,
                            }),
                          "Request updated",
                        )
                      }
                      onPublish={() => setModal("publish-answer")}
                      onChanged={refresh}
                    />
                  ) : (
                    <div className="detail-placeholder">
                      <Icon name="inbox" size={36} />
                      <h3>A little clarity goes a long way.</h3>
                      <p>
                        Select a request to review the context, ask a question,
                        or respond.
                      </p>
                    </div>
                  )}
                </div>
              )}
            </>
          )}
          {page === "conversations" && (
            <div className="conversation-workspace">
              <div className="conversation-list">
                {data.conversations.map((c) => (
                  <button
                    className={selected === c.id ? "selected" : ""}
                    key={c.id}
                    onClick={() => openConversation(c.id)}
                  >
                    <Icon name="chat" />
                    <span>
                      <strong>{c.title}</strong>
                      <small>{timeAgo(c.created_at)}</small>
                    </span>
                    <Icon name="arrow" size={14} />
                  </button>
                ))}
                {data.conversations.length === 0 && (
                  <p className="muted pad">No conversations yet.</p>
                )}
              </div>
              {conversation ? (
                <Chat
                  key={conversation.id}
                  conversation={conversation}
                  me={me}
                  name={name}
                  attachmentRevision={attachmentRevision}
                  onSend={(body, key) =>
                    act(
                      () =>
                        api(
                          "/messages",
                          { conversation_id: conversation.id, body },
                          { headers: { "Idempotency-Key": key, Prefer: "return=minimal" } },
                        ),
                      "Message sent",
                      live !== "Live updates",
                    )
                  }
                />
              ) : (
                <Empty icon="chat" title="Start with a conversation">
                  Message an agent or a teammate, or open a request's
                  conversation.
                </Empty>
              )}
            </div>
          )}
          {page === "agents" && (
            <>
              <FleetSummary />
              <Search
                value={query}
                setValue={setQuery}
                placeholder="Find agents by name or project"
              />
              {data.agents.length === 0 ? (
                <Empty
                  icon="agent"
                  title="Your first agent is one connection away"
                  action={
                    <button
                      className="secondary"
                      onClick={() => setModal("connect-agent")}
                    >
                      View connection instructions
                    </button>
                  }
                >
                  Start a worker with a scoped enrollment token. It appears here
                  as soon as it connects.
                </Empty>
              ) : (
                <div className="agent-layout">
                  <div className="entity-list">
                    <div className="list-heading">
                      <span>AGENT</span>
                      <span>PROJECT</span>
                      <span>STATUS</span>
                      <span />
                    </div>
                    {filtered(data.agents).map((p) => (
                      <button
                        className={
                          "entity-row " + (selected === p.id ? "selected" : "")
                        }
                        key={p.id}
                        onClick={() => setSelected(p.id)}
                      >
                        <div className="entity-name">
                          <Avatar person={p} />
                          <span>
                            <strong>{p.name}</strong>
                            <small>{p.external_id}</small>
                          </span>
                        </div>
                        <span className="muted">
                          {String(p.metadata.project ?? "Workspace")}
                        </span>
                        <Badge
                          tone={
                            p.disabled
                              ? "muted"
                              : p.instances?.some(
                                    (i) => i.presence === "connected",
                                  )
                                ? "green"
                                : "muted"
                          }
                        >
                          {p.disabled
                            ? "Disabled"
                            : p.instances?.some(
                                  (i) => i.presence === "connected",
                                )
                              ? "Connected"
                              : "Offline"}
                        </Badge>
                        <Icon name="arrow" size={16} />
                      </button>
                    ))}
                  </div>
                  {agent && (
                    <aside className="inspector">
                      <div className="inspector-heading">
                        <Avatar person={agent} />
                        <h2>{agent.name}</h2>
                        <button
                          className="icon-button"
                          aria-label="Close agent details"
                          onClick={() => setSelected(null)}
                        >
                          <Icon name="close" />
                        </button>
                      </div>
                      <p className="mono small">{agent.external_id}</p>
                      <button
                        className="primary wide"
                        onClick={() => setModal("message-agent")}
                      >
                        Message {agent.name}
                        <Icon name="send" />
                      </button>
                      <h3>
                        Running instances <Badge>{instances.length}</Badge>
                      </h3>
                      {instances.map((i) => (
                        <div className="instance" key={i.id}>
                          <div>
                            <span className={"presence " + i.presence} />
                            <strong>{i.presence}</strong>
                            <small>{timeAgo(i.last_seen)}</small>
                          </div>
                          <code>{i.id.slice(0, 20)}…</code>
                          <p>
                            {i.capabilities?.join(", ") ||
                              "No capabilities declared"}
                          </p>
                          {Object.keys(i.progress ?? {}).length > 0 && (
                            <pre>{JSON.stringify(i.progress, null, 2)}</pre>
                          )}
                        </div>
                      ))}
                      <div className="info-note">
                        Connected shows reachability. Progress is reported by
                        the worker.
                      </div>
                      {me.admin && (
                        <button
                          className="quiet"
                          onClick={() =>
                            void act(
                              () =>
                                api("/agents/" + agent.id + "/status", {
                                  disabled: !agent.disabled,
                                }),
                              "Agent access updated",
                            )
                          }
                        >
                          {agent.disabled ? "Enable access" : "Disable access"}
                        </button>
                      )}
                    </aside>
                  )}
                </div>
              )}
            </>
          )}
          {page === "people" && (
            <>
              <Search
                value={query}
                setValue={setQuery}
                placeholder="Find people by name or department"
              />
              <div className="entity-list">
                <div className="list-heading people">
                  <span>PERSON</span>
                  <span>DEPARTMENT</span>
                  <span>MANAGER</span>
                  <span>STATUS</span>
                </div>
                {filtered(data.people).map((p) => (
                  <div className="entity-row people" key={p.id}>
                    <div className="entity-name">
                      <Avatar person={p} />
                      <span>
                        <strong>{p.name}</strong>
                        {me.admin && (
                          <button
                            className="quiet small"
                            onClick={() => {
                              setSelected(p.id);
                              setModal("edit-person");
                            }}
                          >
                            Edit profile
                          </button>
                        )}
                        <small>
                          {String(p.metadata.email ?? p.external_id)}
                        </small>
                      </span>
                    </div>
                    <span>{String(p.metadata.department || "Unassigned")}</span>
                    <span className="muted">
                      {p.metadata.manager_id
                        ? name(String(p.metadata.manager_id))
                        : "None"}
                    </span>
                    <button
                      className="quiet"
                      disabled={!me.admin || p.id === me.person.id}
                      onClick={() =>
                        void act(
                          () =>
                            api("/people/" + p.id + "/status", {
                              disabled: !p.disabled,
                            }),
                          "Person access updated",
                        )
                      }
                    >
                      <Badge tone={p.disabled ? "muted" : "green"}>
                        {p.disabled ? "Disabled" : "Active"}
                      </Badge>
                    </button>
                  </div>
                ))}
              </div>
              <p className="footnote">
                People can be added manually. Directory import and organization
                sync are an enterprise integration; basic OIDC login belongs to
                the core.
              </p>
            </>
          )}
          {page === "groups" && (
            <div className="simple-list">
              {data.groups.length === 0 ? (
                <Empty icon="group" title="A clear audience for every message">
                  Create a group, add its members, and assign permissions
                  separately.
                </Empty>
              ) : (
                data.groups.map((g) => (
                  <div className="group-row" key={g.id}>
                    <div className="round-icon">
                      <Icon name="group" />
                    </div>
                    <div>
                      <h3>{g.name}</h3>
                      <p>
                        {g.members.map(name).join(", ") || "No members yet"}
                      </p>
                    </div>
                    <Badge>{g.members.length} members</Badge>
                    <button
                      className="secondary"
                      onClick={() => {
                        setSelected(g.id);
                        setModal("message-group");
                      }}
                    >
                      Message group
                    </button>
                    {me.admin && (
                      <button
                        className="quiet"
                        onClick={() => {
                          setSelected(g.id);
                          setModal("edit-group");
                        }}
                      >
                        Edit
                      </button>
                    )}
                  </div>
                ))
              )}
            </div>
          )}
          {page === "permissions" && (
            <>
              <div className="info-strip">
                <Icon name="key" />
                <span>
                  Membership defines who belongs. A role defines what they may
                  do. Receiving a message grants no new authority.
                </span>
              </div>
              {data.roles.length === 0 ? (
                <Empty icon="key" title="Permission starts with a role">
                  Create a small set of rules, then assign the role to a person,
                  agent or group.
                </Empty>
              ) : (
                data.roles.map((role) => (
                  <section className="role-section" key={role.id}>
                    <header>
                      <h2>{role.name}</h2>
                      <div>
                        <button
                          className="quiet"
                          onClick={() => {
                            setSelected(role.id);
                            setModal("edit-role");
                          }}
                        >
                          Edit rules
                        </button>
                        <button
                          className="secondary"
                          onClick={() => {
                            setSelected(role.id);
                            setModal("bind-role");
                          }}
                        >
                          Assign role
                        </button>
                      </div>
                    </header>
                    <div className="rules">
                      {role.rules.map((rule, i) => (
                        <div className="rule" key={i}>
                          <Badge tone="green">{rule.action}</Badge>
                          <span>
                            {rule.target_kind === "*"
                              ? "Any type"
                              : rule.target_kind}
                          </span>
                          <span className="rule-target">
                            {rule.target_id === "$self"
                              ? "Their own identity"
                              : rule.target_id === "*"
                                ? "Any permitted target"
                                : name(rule.target_id)}
                          </span>
                          {rule.action_name && <code>{rule.action_name}</code>}
                        </div>
                      ))}
                    </div>
                    <div className="assigned">
                      Assigned to{" "}
                      {data.bindings.filter((b) => b.role_id === role.id)
                        .length === 0
                        ? "nobody yet"
                        : data.bindings
                            .filter((b) => b.role_id === role.id)
                            .map((b) => (
                              <button
                                className="binding-chip"
                                key={b.subject_id}
                                onClick={() =>
                                  void act(
                                    () =>
                                      api("/role-bindings", {
                                        ...b,
                                        remove: true,
                                      }),
                                    "Role removed",
                                  )
                                }
                                aria-label={
                                  "Remove role from " +
                                  (data.groups.find(
                                    (g) => g.id === b.subject_id,
                                  )?.name ?? name(b.subject_id))
                                }
                              >
                                {data.groups.find((g) => g.id === b.subject_id)
                                  ?.name ?? name(b.subject_id)}{" "}
                                ×
                              </button>
                            ))}
                    </div>
                  </section>
                ))
              )}
              <button className="secondary" onClick={() => setModal("explain")}>
                Check effective permission
              </button>
            </>
          )}
          {page === "connections" && (
            <>
              <div className="native-connection">
                <span className="connection-symbol">
                  <Icon name="chat" size={24} />
                </span>
                <div>
                  <h3>Web inbox</h3>
                  <p>
                    Included for every person. Uses the same requests and
                    permissions.
                  </p>
                </div>
                <Badge tone="green">Available</Badge>
              </div>
              <div className="simple-list">
                {data.connections.map((c) => (
                  <div className="group-row" key={c.id}>
                    <span className="connection-symbol">
                      <Icon name="plug" />
                    </span>
                    <div>
                      <h3>{c.name}</h3>
                      <p>
                        {c.kind} ·{" "}
                        {c.secret_configured
                          ? "Credentials configured"
                          : "Needs credentials"}
                      </p>
                    </div>
                    <button
                      className="quiet"
                      onClick={() =>
                        void act(
                          () =>
                            api("/connections", { ...c, enabled: !c.enabled }),
                          "Connection updated",
                        )
                      }
                    >
                      <Badge tone={c.enabled ? "green" : "muted"}>
                        {c.enabled ? "Enabled" : "Paused"}
                      </Badge>
                    </button>
                    <button
                      className="secondary"
                      onClick={() => {
                        setSelected(c.id);
                        setModal("destination");
                      }}
                    >
                      Add destination
                    </button>
                    <button
                      className="quiet"
                      onClick={() => {
                        setSelected(c.id);
                        setModal("edit-connection");
                      }}
                    >
                      Edit
                    </button>
                  </div>
                ))}
              </div>
              {data.destinations.map((d) => (
                <div className="destination-row" key={d.id}>
                  <strong>{name(d.principal_id)}</strong>
                  <span>
                    {
                      data.connections.find((c) => c.id === d.connection_id)
                        ?.name
                    }
                  </span>
                  <code>{d.address}</code>
                  <Badge>{d.mode}</Badge>
                  <button
                    className="quiet"
                    onClick={() =>
                      void act(
                        () => api("/destinations/" + d.id + "/remove", {}),
                        "Destination removed",
                      )
                    }
                  >
                    Remove
                  </button>
                </div>
              ))}
              <div className="info-note">
                Email approvals open an authenticated web page. External
                provider setup requires your own account and credentials.
              </div>
            </>
          )}
          {page === "activity" && (
            <Activity data={data} name={name} onRefresh={refresh} act={act} />
          )}
          {page === "settings" && (
            <Settings me={me} people={entities} act={act} />
          )}
        </div>
        <footer className="app-footer">
          <span>
            notif. <span>Communication with context.</span>
          </span>
          <span>Open source · 0.1.0-dev</span>
        </footer>
      </main>
      {modal && (
        <Modal title={modalTitle(modal)} onClose={() => setModal(null)}>
          {modal === "publish-answer" && current ? (
            <PublishAnswerForm
              request={current}
              people={entities}
              onDone={() => {
                setModal(null);
                void refresh();
              }}
            />
          ) : (
            <Forms
              kind={modal}
              data={data}
              me={me}
              selected={selected}
              act={act}
              close={() => setModal(null)}
              openConversation={openConversation}
            />
          )}
        </Modal>
      )}
    </div>
  );
}
function Search({
  value,
  setValue,
  placeholder,
}: {
  value: string;
  setValue: (s: string) => void;
  placeholder: string;
}) {
  return (
    <div className="search">
      <Icon name="search" />
      <input
        aria-label={placeholder}
        placeholder={placeholder}
        value={value}
        onChange={(e) => setValue(e.target.value)}
      />
    </div>
  );
}
function RequestDetail({
  request: r,
  sender,
  me,
  position,
  total,
  onClose,
  onConversation,
  onRespond,
  onEnd,
  onPublish,
  onChanged,
  matchingCount,
  matchingNames,
}: {
  matchingCount: number;
  matchingNames: string[];
  request: RequestItem;
  sender?: Entity;
  me: Me;
  position: number;
  total: number;
  onClose: () => void;
  onConversation: () => void;
  onRespond: (
    outcome: string,
    answer?: unknown,
    share?: boolean,
  ) => Promise<boolean>;
  onEnd: (state: string) => Promise<boolean>;
  onPublish: () => void;
  onChanged: () => void;
}) {
  const [answer, setAnswer] = useState(""),
    [busy, setBusy] = useState(false),
    [share, setShare] = useState(false);
  const detail = useRef<HTMLElement>(null);
  useEffect(() => {
    setAnswer("");
    setShare(false);
    if (window.matchMedia?.("(max-width: 1120px)").matches) {
      detail.current?.scrollIntoView({block:"start",behavior:"auto"});
      detail.current?.focus({preventScroll:true});
    }
  }, [r.id]);
  async function respond(outcome: string, value?: unknown) {
    setBusy(true);
    await onRespond(outcome, value, share);
    setBusy(false);
  }
  return (
    <section className="request-detail" ref={detail} tabIndex={-1} aria-label="Request details">
      <div className="detail-top">
        <span>
          {r.kind.toUpperCase()}
          {r.state === "open" && total > 0 && ` · ${position || 1} OF ${total}`}
        </span>
        <button
          className="icon-button"
          aria-label="Close request"
          onClick={onClose}
        >
          <Icon name="close" />
        </button>
      </div>
      <div className="detail-sender">
        <Avatar person={sender} />
        <span>
          <strong>{sender?.name ?? "Sender"}</strong>
          <small>{timeAgo(r.created_at)}</small>
        </span>
      </div>
      <h2>{r.title}</h2>
      {r.requester_instance_id && (
        <p className="small muted">
          Worker <code>{r.requester_instance_id}</code>
        </p>
      )}
      {r.response?.source_invalidated && (
        <p role="status" className="error">
          The shared answer used here was invalidated. Review before relying on
          it.
        </p>
      )}
      {r.body && <p className="request-body">{r.body}</p>}
      {r.action && (
        <div className="action-binding">
          <strong>Proposed action</strong>
          <pre>{JSON.stringify(r.action, null, 2)}</pre>
        </div>
      )}
      <div className="request-context">
        <span>Deadline</span>
        <strong>{new Date(r.expires_at).toLocaleString()}</strong>
        <span>Status</span>
        <Badge tone={r.state === "open" ? "green" : "muted"}>
          {requestLabel(r)}
        </Badge>
      </div>
      <button className="context-link" onClick={onConversation}>
        <Icon name="chat" />
        Ask for detail in the conversation
        <Icon name="arrow" size={14} />
      </button>
      {r.state === "open" ? (
        <div className="answer-area">
          {matchingCount > 1 && (
            <div className="small muted">
              <p>Matching requesters: {matchingNames.join(", ")}</p>
              <p>
                Scope key: <code>{r.shared_key}</code>
              </p>
            </div>
          )}
          {(me.can_publish_answers ?? me.admin) &&
            matchingCount > 1 &&
            r.kind === "question" && (
              <label className="check-row shared-question-choice">
                <input
                  type="checkbox"
                  checked={share}
                  onChange={(e) => setShare(e.target.checked)}
                />
                Answer all {matchingCount} matching questions and share with
                these requesters for 24 hours.
              </label>
            )}
          {r.kind === "question" && (
            <AnswerSuggestions request={r} onChanged={onChanged} />
          )}{" "}
          {r.kind === "approval" ? (
            <div className="decision-buttons">
              <button
                className="primary"
                disabled={busy}
                onClick={() => void respond("approved")}
              >
                <Icon name="check" />
                Approve
              </button>
              <button
                className="secondary"
                disabled={busy}
                onClick={() => void respond("denied")}
              >
                Deny
              </button>
            </div>
          ) : r.choices?.length ? (
            <div className="choices">
              {r.choices.map((c) => (
                <button
                  className="choice"
                  key={c}
                  disabled={busy}
                  onClick={() => void respond("answered", c)}
                >
                  {c}
                  <Icon name="arrow" size={14} />
                </button>
              ))}
            </div>
          ) : (
            <form
              onSubmit={(e) => {
                e.preventDefault();
                void respond(
                  r.kind === "task" ? "completed" : "answered",
                  answer,
                );
              }}
            >
              <Field label="Your response">
                <textarea
                  value={answer}
                  onChange={(e) => setAnswer(e.target.value)}
                  placeholder="Add your answer…"
                  required
                  rows={3}
                />
              </Field>
              <button className="primary wide" disabled={busy}>
                Send response
                <Icon name="send" />
              </button>
            </form>
          )}
          {(me.admin || me.person.id === r.requester_id) && (
            <div className="request-admin">
              <button className="quiet" onClick={() => void onEnd("cancel")}>
                Cancel request
              </button>
              <button className="quiet" onClick={() => void onEnd("supersede")}>
                Mark superseded
              </button>
            </div>
          )}
          <p className="small muted">
            Asking for clarification keeps this request open.
          </p>
        </div>
      ) : (
        <div className="resolved-state">
          <Icon name="check" />
          <strong>{requestLabel(r)}</strong>
          {r.response?.answer !== undefined && (
            <p>
              {typeof r.response.answer === "string"
                ? r.response.answer
                : JSON.stringify(r.response.answer)}
            </p>
          )}
        </div>
      )}
      {me.admin &&
        r.kind === "question" &&
        r.response?.outcome === "answered" && (
          <button className="secondary wide" onClick={onPublish}>
            Publish a scoped reusable answer
          </button>
        )}
      <code className="request-id">{r.id}</code>
    </section>
  );
}
function Chat({
  conversation: c,
  me,
  name,
  onSend,
  attachmentRevision,
}: {
  conversation: Conversation;
  me: Me;
  name: (id: string) => string;
  onSend: (body: string, key: string) => Promise<boolean>;
  attachmentRevision: number;
}) {
  const sendAttempt = useRef({ body: "", key: "" });
  const draftKey = "notif-draft/" + me.person.id + "/" + c.id;
  const [body, setBody] = useState(
      () => sessionStorage.getItem(draftKey) ?? "",
    ),
    [busy, setBusy] = useState(false),
    end = useRef<HTMLDivElement>(null);
  useEffect(() => {
    setBody(sessionStorage.getItem(draftKey) ?? "");
  }, [draftKey]);
  useEffect(() => {
    end.current?.scrollIntoView({ block: "nearest" });
  }, [c.messages?.at(-1)?.id]);
  return (
    <section className="chat">
      <div className="chat-heading">
        <span className="round-icon">
          <Icon name="chat" />
        </span>
        <div>
          <h2>{c.title}</h2>
          <p>{c.participants?.map((x) => x.name).join(" · ")}</p>
        </div>
      </div>
      <div className="chat-history">
        {c.messages?.length === 0 && (
          <div className="chat-start">
            This conversation is ready. Ask for context or send an update.
          </div>
        )}
        {c.messages?.map((m) => (
          <ChatMessage key={m.id} message={m} sender={name(m.sender_id)} mine={m.sender_id === me.person.id} />
        ))}
        <div ref={end} />
      </div>
      <Attachments conversation={c.id} revision={attachmentRevision} />
      <form
        className="composer"
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          if (sendAttempt.current.body !== body || !sendAttempt.current.key)
            sendAttempt.current = { body, key: crypto.randomUUID() };
          if (await onSend(body, sendAttempt.current.key)) {
            sendAttempt.current = { body: "", key: "" };
            setBody("");
            sessionStorage.removeItem(draftKey);
          }
          setBusy(false);
        }}
      >
        <textarea
          rows={2}
          aria-label="Message"
          placeholder="Write a message…"
          value={body}
          onChange={(e) => {
            setBody(e.target.value);
            sessionStorage.setItem(draftKey, e.target.value);
          }}
          maxLength={32768}
          required
        />
        <div>
          <span>Messages preserve every pending request.</span>
          <button className="primary" disabled={busy || !body.trim()}>
            {busy ? "Sending…" : "Send"}
            <Icon name="send" size={15} />
          </button>
        </div>
      </form>
    </section>
  );
}
const ChatMessage = memo(function ChatMessage({ message: m, sender, mine }: { message: Message; sender: string; mine: boolean }) {
  return <article className={"message " + (mine ? "mine" : "")}>
    <div className="message-meta">
      <strong>{sender}</strong>
      <span>{timeAgo(m.created_at)}</span>
      {m.instance_id && <Badge>worker</Badge>}
    </div>
    <p>{m.body}</p>
  </article>;
});
function modalTitle(k: string) {
  return (
    (
      {
        person: "Add a person",
        "edit-person": "Edit person",
        group: "Create a group",
        "edit-group": "Edit group",
        role: "Create a role",
        "edit-role": "Edit role",
        "bind-role": "Assign role",
        connection: "Add a connection",
        "edit-connection": "Edit connection",
        destination: "Add destination",
        compose: "New conversation",
        ask: "Ask a question",
        "message-agent": "Message agent",
        "message-group": "Message group",
        "connect-agent": "Connect an agent",
        explain: "Check effective permission",
        "publish-answer": "Publish a shared answer",
      } as Record<string, string>
    )[k] ?? k
  );
}
type FormsProps = {
  kind: string;
  data: Data;
  me: Me;
  selected: string | null;
  act: (fn: () => Promise<unknown>, notice?: string) => Promise<boolean>;
  close: () => void;
  openConversation: (id: string) => void;
};
function Forms({
  kind,
  data,
  me,
  selected,
  act,
  close,
  openConversation,
}: FormsProps) {
  const entities = [...data.people, ...data.agents, ...data.services];
  const existingPerson = data.people.find((p) => p.id === selected);
  const existingGroup = data.groups.find((x) => x.id === selected);
  const existingRole = data.roles.find((x) => x.id === selected);
  const existingConnection = data.connections.find((x) => x.id === selected);
  const [name, setName] = useState(
      kind === "edit-person"
        ? (existingPerson?.name ?? "")
        : kind === "edit-group"
          ? (existingGroup?.name ?? "")
          : kind === "edit-role"
            ? (existingRole?.name ?? "")
            : kind === "edit-connection"
              ? (existingConnection?.name ?? "")
              : "",
    ),
    [external, setExternal] = useState(""),
    [department, setDepartment] = useState(
      kind === "edit-person"
        ? String(existingPerson?.metadata.department ?? "")
        : "",
    ),
    [manager, setManager] = useState(
      kind === "edit-person"
        ? String(existingPerson?.metadata.manager_id ?? "")
        : "",
    ),
    [email, setEmail] = useState(
      kind === "edit-person"
        ? String(existingPerson?.metadata.email ?? "")
        : "",
    ),
    [members, setMembers] = useState<string[]>(
      kind === "edit-group" ? (existingGroup?.members ?? []) : [],
    ),
    [rules, setRules] = useState<Rule[]>(
      kind === "edit-role"
        ? (existingRole?.rules ?? [])
        : [{ action: "view", target_kind: "agent", target_id: "*" }],
    ),
    [target, setTarget] = useState(
      selected && kind.startsWith("message-") ? selected : "",
    ),
    [body, setBody] = useState(""),
    [type, setType] = useState(
      kind === "edit-connection"
        ? (existingConnection?.kind ?? "webhook")
        : "webhook",
    ),
    [config, setConfig] = useState<Record<string, string>>(
      kind === "edit-connection" ? (existingConnection?.config ?? {}) : {},
    ),
    [secret, setSecret] = useState(""),
    [address, setAddress] = useState(""),
    [busy, setBusy] = useState(false),
    [result, setResult] = useState<unknown>(null),
    [action, setAction] = useState("message"),
    [subject, setSubject] = useState(""),
    [connectionID, setConnectionID] = useState(""),
    [actionName, setActionName] = useState(""),
    [signingSecret, setSigningSecret] = useState(""),
    [mode, setMode] = useState("primary");
  const targetOptions = [
    ...entities.map((e) => ({ id: e.id, label: e.name, kind: e.kind })),
    ...data.groups.map((g) => ({ id: g.id, label: g.name, kind: "group" })),
  ];
  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    const success = await act(
      async () => {
        if (kind === "edit-person")
          await api("/people/" + selected + "/profile", {
            name,
            department,
            manager_id: manager,
            email,
          });
        else if (kind === "person")
          await api("/people", {
            name,
            external_id: external || name.toLowerCase().replaceAll(" ", "-"),
            department,
            manager_id: manager,
            email,
          });
        else if (kind === "group" || kind === "edit-group")
          await api("/groups", {
            id: kind === "edit-group" ? selected : undefined,
            name,
            members,
          });
        else if (kind === "role" || kind === "edit-role")
          await api("/roles", {
            id: kind === "edit-role" ? selected : undefined,
            name,
            rules,
          });
        else if (kind === "bind-role")
          await api("/role-bindings", {
            role_id: selected,
            subject_kind: data.groups.some((g) => g.id === target)
              ? "group"
              : "principal",
            subject_id: target,
          });
        else if (kind === "connection" || kind === "edit-connection")
          await api("/connections", {
            id: kind === "edit-connection" ? selected : undefined,
            name,
            kind: type,
            config,
            secret:
              type === "slack" && secret
                ? JSON.stringify({
                    bot_token: secret,
                    signing_secret: signingSecret,
                  })
                : secret,
            enabled: true,
          });
        else if (kind === "destination")
          await api("/destinations", {
            connection_id: selected,
            principal_id: target,
            address,
            mode,
          });
        else if (kind === "explain") {
          const option = targetOptions.find((t) => t.id === target)!;
          setResult(
            await api("/permissions/explain", {
              action,
              subject_id: subject,
              connection_id: connectionID,
              action_name: actionName,
              target: { kind: option.kind, id: target },
            }),
          );
          return;
        } else {
          const option = targetOptions.find((t) => t.id === target)!;
          const t: Target = { kind: option.kind, id: target };
          if (kind === "ask") {
            await api("/requests", {
              target: t,
              title: name,
              body,
              kind: "question",
              choices: external
                ? external
                    .split(",")
                    .map((x) => x.trim())
                    .filter(Boolean)
                : [],
            });
          } else {
            const result = await api<{ conversation_id: string }>("/messages", {
              target: t,
              body,
              title: name,
            }, { headers: { Prefer: "return=minimal" } });
            openConversation(result.conversation_id);
          }
        }
      },
      kind === "ask" ? "Question queued" : "Saved",
    );
    setBusy(false);
    if (success && kind !== "explain") close();
  }
  if (kind === "connect-agent")
    return (
      <div className="modal-body">
        <p>
          Use an enrollment credential with an allowed namespace. Each
          connection creates an independently authenticated worker instance.
        </p>
        <ol className="setup-steps">
          <li>
            Create a launcher person or service and assign its worker role.
          </li>
          <li>
            In Settings, issue an enrollment token restricted to a prefix such
            as <code>research/</code>.
          </li>
          <li>Connect using a Notif SDK. Agents appear automatically.</li>
        </ol>
        <pre className="code-block">{`from notif import Client\n\nlauncher = Client("http://localhost:4010", TOKEN)\nworker, session = launcher.connect(\n    "research/scout", "Scout",\n    capabilities=["message", "status"]\n)`}</pre>
        <p className="muted small">
          For a local transport example, run{" "}
          <code>go run ./cmd/notif demo --name Scout --ask</code> from the
          project. It uses the local owner token to enroll, then a
          worker-specific token.
        </p>
        <button className="primary wide" onClick={close}>
          Done
        </button>
      </div>
    );
  return (
    <form className="modal-body" onSubmit={submit}>
      {[
        "person",
        "edit-person",
        "group",
        "edit-group",
        "role",
        "edit-role",
        "connection",
        "edit-connection",
        "ask",
      ].includes(kind) && (
        <Field label={kind === "ask" ? "Question" : "Name"}>
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={
              kind === "person"
                ? "Alex Morgan"
                : kind === "ask"
                  ? "What do you need to know?"
                  : "A clear, recognizable name"
            }
            required
            autoFocus
          />
        </Field>
      )}
      {(kind === "person" || kind === "edit-person") && (
        <>
          <Field label="Your person ID">
            <input
              value={external}
              onChange={(e) => setExternal(e.target.value)}
              placeholder="employee-123"
            />
          </Field>
          <Field label="Email">
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          </Field>
          <div className="form-columns">
            <Field label="Department">
              <input
                value={department}
                onChange={(e) => setDepartment(e.target.value)}
              />
            </Field>
            <Field label="Manager">
              <select
                value={manager}
                onChange={(e) => setManager(e.target.value)}
              >
                <option value="">No manager</option>
                {data.people.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </Field>
          </div>
        </>
      )}
      {(kind === "group" || kind === "edit-group") && (
        <div className="member-picker">
          <strong>Members</strong>
          {(["person", "agent"] as const).map((k) => (
            <div key={k}>
              <span className="eyebrow">
                {k === "person" ? "PEOPLE" : "AGENTS"}
              </span>
              {entities
                .filter((e) => e.kind === k)
                .map((p) => (
                  <label className="check-row" key={p.id}>
                    <input
                      type="checkbox"
                      checked={members.includes(p.id)}
                      onChange={(e) =>
                        setMembers(
                          e.target.checked
                            ? [...members, p.id]
                            : members.filter((x) => x !== p.id),
                        )
                      }
                    />
                    <Avatar person={p} />
                    {p.name}
                  </label>
                ))}
            </div>
          ))}
        </div>
      )}
      {(kind === "role" || kind === "edit-role") && (
        <>
          <p className="muted small">
            Rules permit actions. Missing rules deny them. Assign the role after
            saving.
          </p>
          {rules.map((r, i) => (
            <div className="rule-editor" key={i}>
              <Field label="Can">
                <select
                  value={r.action}
                  onChange={(e) =>
                    setRules(
                      rules.map((x, j) =>
                        j === i ? { ...x, action: e.target.value } : x,
                      ),
                    )
                  }
                >
                  {["view", "message", "ask", "decide", "manage"].map((x) => (
                    <option key={x}>{x}</option>
                  ))}
                </select>
              </Field>
              <Field label="Target type">
                <select
                  value={r.target_kind}
                  onChange={(e) =>
                    setRules(
                      rules.map((x, j) =>
                        j === i
                          ? {
                              ...x,
                              target_kind: e.target.value,
                              target_id: "*",
                            }
                          : x,
                      ),
                    )
                  }
                >
                  {["person", "agent", "group", "service", "*"].map((x) => (
                    <option key={x} value={x}>
                      {x === "*" ? "Any type" : x}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="Target">
                <select
                  value={r.target_id}
                  onChange={(e) =>
                    setRules(
                      rules.map((x, j) =>
                        j === i ? { ...x, target_id: e.target.value } : x,
                      ),
                    )
                  }
                >
                  <option value="*">Any target</option>
                  <option value="$self">Their own identity (self)</option>
                  {targetOptions
                    .filter(
                      (t) => t.kind === r.target_kind || r.target_kind === "*",
                    )
                    .map((t) => (
                      <option key={t.id} value={t.id}>
                        {t.label}
                      </option>
                    ))}
                </select>
              </Field>
              <Field label="Connection (optional)">
                <select
                  value={r.connection_id ?? ""}
                  onChange={(e) =>
                    setRules(
                      rules.map((x, j) =>
                        j === i ? { ...x, connection_id: e.target.value } : x,
                      ),
                    )
                  }
                >
                  <option value="">Any allowed transport</option>
                  {data.connections.map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.name}
                    </option>
                  ))}
                </select>
              </Field>
              <Field label="Action name (optional)">
                <input
                  value={r.action_name ?? ""}
                  onChange={(e) =>
                    setRules(
                      rules.map((x, j) =>
                        j === i ? { ...x, action_name: e.target.value } : x,
                      ),
                    )
                  }
                  placeholder="deploy"
                />
              </Field>
              {["ask", "decide"].includes(r.action) && (
                <details className="rule-conditions">
                  <summary>Resource and amount limits</summary>
                  <Field label="Exact resource (optional)">
                    <input
                      value={r.conditions?.resource ?? ""}
                      onChange={(e) =>
                        setRules(
                          rules.map((x, j) =>
                            j === i
                              ? {
                                  ...x,
                                  conditions: {
                                    ...x.conditions,
                                    resource: e.target.value,
                                  },
                                }
                              : x,
                          ),
                        )
                      }
                    />
                  </Field>
                  <Field label="Resource version (optional)">
                    <input
                      value={r.conditions?.resource_version ?? ""}
                      onChange={(e) =>
                        setRules(
                          rules.map((x, j) =>
                            j === i
                              ? {
                                  ...x,
                                  conditions: {
                                    ...x.conditions,
                                    resource_version: e.target.value,
                                  },
                                }
                              : x,
                          ),
                        )
                      }
                    />
                  </Field>
                  <Field label="Maximum amount in minor units">
                    <input
                      type="number"
                      min="0"
                      step="1"
                      value={r.conditions?.max_amount_minor ?? ""}
                      onChange={(e) =>
                        setRules(
                          rules.map((x, j) =>
                            j === i
                              ? {
                                  ...x,
                                  conditions: {
                                    ...x.conditions,
                                    max_amount_minor: e.target.value
                                      ? Number(e.target.value)
                                      : undefined,
                                  },
                                }
                              : x,
                          ),
                        )
                      }
                    />
                  </Field>
                  <Field label="Currency (required with amount)">
                    <input
                      maxLength={3}
                      value={r.conditions?.currency ?? ""}
                      onChange={(e) =>
                        setRules(
                          rules.map((x, j) =>
                            j === i
                              ? {
                                  ...x,
                                  conditions: {
                                    ...x.conditions,
                                    currency: e.target.value.toUpperCase(),
                                  },
                                }
                              : x,
                          ),
                        )
                      }
                    />
                  </Field>
                  <small>
                    For USD, 10000 minor units means $100.00. The action must
                    provide amount_minor and currency.
                  </small>
                </details>
              )}
              <button
                className="icon-button"
                type="button"
                aria-label="Remove rule"
                onClick={() => setRules(rules.filter((_, j) => j !== i))}
              >
                <Icon name="close" />
              </button>
            </div>
          ))}
          <button
            className="secondary"
            type="button"
            onClick={() =>
              setRules([
                ...rules,
                { action: "message", target_kind: "agent", target_id: "*" },
              ])
            }
          >
            <Icon name="plus" />
            Add rule
          </button>
        </>
      )}
      {[
        "compose",
        "ask",
        "message-agent",
        "message-group",
        "bind-role",
        "destination",
        "explain",
      ].includes(kind) && (
        <Field
          label={
            kind === "bind-role"
              ? "Assign to"
              : kind === "destination"
                ? "Person or agent"
                : "To"
          }
        >
          <select
            value={target}
            onChange={(e) => setTarget(e.target.value)}
            required
          >
            <option value="">Choose a target</option>
            {targetOptions
              .filter((t) => kind !== "destination" || t.kind !== "group")
              .map((t) => (
                <option key={t.id} value={t.id}>
                  {t.label} · {t.kind}
                </option>
              ))}
          </select>
        </Field>
      )}
      {["compose", "message-agent", "message-group", "ask"].includes(kind) && (
        <Field label={kind === "ask" ? "Context" : "Message"}>
          <textarea
            rows={4}
            value={body}
            onChange={(e) => setBody(e.target.value)}
            required={kind !== "ask"}
            placeholder="Add the context they need…"
          />
        </Field>
      )}
      {kind === "ask" && (
        <Field label="Choices (optional, separated by commas)">
          <input
            value={external}
            onChange={(e) => setExternal(e.target.value)}
            placeholder="Option one, Option two"
          />
        </Field>
      )}
      {(kind === "connection" || kind === "edit-connection") && (
        <>
          <Field label="Provider">
            <select
              value={type}
              disabled={kind === "edit-connection"}
              onChange={(e) => {
                setType(e.target.value);
                setConfig({});
              }}
            >
              <option value="webhook">Signed webhook</option>
              <option value="slack">Slack</option>
              <option value="email">Email (SMTP)</option>
            </select>
          </Field>
          {(type === "webhook"
            ? ["url"]
            : type === "slack"
              ? ["team_id", "default_agent_id"]
              : ["smtp_addr", "from", "username"]
          ).map((k) => (
            <Field key={k} label={k.replaceAll("_", " ")}>
              <input
                value={config[k] ?? ""}
                onChange={(e) => setConfig({ ...config, [k]: e.target.value })}
                required={k !== "username" && k !== "default_agent_id"}
                placeholder={
                  k === "url"
                    ? "https://your-server.example/events"
                    : k === "smtp_addr"
                      ? "localhost:1125"
                      : k === "from"
                        ? "notifications@example.com"
                        : ""
                }
              />
            </Field>
          ))}
          {type === "webhook" && (
            <Field label="Incoming webhook sender (optional)">
              <select
                value={config.inbound_sender_id ?? ""}
                onChange={(e) =>
                  setConfig({ ...config, inbound_sender_id: e.target.value })
                }
              >
                <option value="">Outgoing only</option>
                {data.services.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </Field>
          )}
          {type === "slack" && (
            <Field label="Slack signing secret">
              <input
                type="password"
                value={signingSecret}
                onChange={(e) => setSigningSecret(e.target.value)}
                required={Boolean(secret) || kind !== "edit-connection"}
                autoComplete="new-password"
              />
            </Field>
          )}
          <Field
            label={
              type === "slack"
                ? "Bot token (server only)"
                : type === "email"
                  ? "SMTP password (only for authenticated relays)"
                  : "Signing secret"
            }
          >
            <input
              type="password"
              minLength={type === "email" ? undefined : 16}
              value={secret}
              onChange={(e) => setSecret(e.target.value)}
              required={type !== "email" && kind !== "edit-connection"}
              autoComplete="new-password"
              placeholder={
                kind === "edit-connection"
                  ? "Leave blank to keep current credential"
                  : ""
              }
            />
          </Field>
        </>
      )}
      {kind === "destination" && (
        <Field label="Delivery mode">
          <select value={mode} onChange={(e) => setMode(e.target.value)}>
            <option value="primary">Primary (replaces current primary)</option>
            <option value="fallback">Fallback after definite failure</option>
            <option value="copy">Explicit additional copy</option>
          </select>
        </Field>
      )}
      {kind === "destination" && (
        <Field label="Destination address">
          <input
            value={address}
            onChange={(e) => setAddress(e.target.value)}
            placeholder="Slack channel ID, email address, or webhook destination label"
            required
          />
        </Field>
      )}
      {kind === "explain" && (
        <>
          <Field label="Whose role permissions?">
            <select
              value={subject}
              onChange={(e) => setSubject(e.target.value)}
            >
              <option value="">My current credential</option>
              {entities.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} · {p.kind}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Connection">
            <select
              value={connectionID}
              onChange={(e) => setConnectionID(e.target.value)}
            >
              <option value="">Native web / worker</option>
              {data.connections.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Action name">
            <input
              value={actionName}
              onChange={(e) => setActionName(e.target.value)}
              placeholder="deploy"
            />
          </Field>
        </>
      )}
      {kind === "explain" && (
        <Field label="Action">
          <select value={action} onChange={(e) => setAction(e.target.value)}>
            {["view", "message", "ask", "decide", "manage"].map((x) => (
              <option key={x}>{x}</option>
            ))}
          </select>
        </Field>
      )}
      {result !== null && (
        <pre className="result-panel">{JSON.stringify(result, null, 2)}</pre>
      )}
      <div className="form-footer">
        <button type="button" className="secondary" onClick={close}>
          Cancel
        </button>
        <button className="primary" disabled={busy}>
          {busy
            ? "Saving…"
            : kind === "explain"
              ? "Check permission"
              : kind === "ask"
                ? "Ask question"
                : kind.includes("message") || kind === "compose"
                  ? "Send message"
                  : "Save"}
        </button>
      </div>
      <p className="small muted">
        Changes apply to {me.company} · {me.environment}.
      </p>
    </form>
  );
}
function Activity({
  data,
  name,
  onRefresh,
  act,
}: {
  data: Data;
  name: (id: string) => string;
  onRefresh: () => Promise<void>;
  act: FormsProps["act"];
}) {
  const [view, setView] = useState("events"),
    [deliveries, setDeliveries] = useState<Record<string, unknown>[]>([]);
  useEffect(() => {
    if (view !== "deliveries") return;
    const abort = new AbortController();
    const load = () => void api<Record<string, unknown>[]>("/deliveries", undefined, { signal: abort.signal })
      .then(value => { if (!abort.signal.aborted) setDeliveries(value); })
      .catch(() => { if (!abort.signal.aborted) setDeliveries([]); });
    load();
    return () => abort.abort();
  }, [view, data.events]);
  return (
    <>
      <div className="section-toolbar">
        <div className="tabs">
          <button
            className={view === "events" ? "selected" : ""}
            onClick={() => setView("events")}
          >
            Events
          </button>
          <button
            className={view === "deliveries" ? "selected" : ""}
            onClick={() => setView("deliveries")}
          >
            Deliveries & recovery
          </button>
        </div>
        <button className="secondary" onClick={() => void onRefresh()}>
          Refresh
        </button>
      </div>
      {view === "events" ? (
        <div className="activity-list">
          {[...data.events].reverse().map((e) => (
            <div className="activity-row" key={e.sequence}>
              <span className="activity-node" />
              <span>
                <strong>{e.kind.replaceAll(".", " · ")}</strong>
                <small>
                  {name(e.actor_id)} · #{e.sequence}
                </small>
              </span>
              <time>{timeAgo(e.created_at)}</time>
            </div>
          ))}
          {data.events.length === 0 && (
            <Empty icon="pulse" title="Activity appears here">
              Send a message or connect a worker to see the full interaction
              history.
            </Empty>
          )}
        </div>
      ) : (
        <div className="simple-list">
          {deliveries.map((d) => (
            <div className="delivery-row" key={String(d.id)}>
              <span>
                <strong>{String(d.operation_id).slice(0, 22)}…</strong>
                <small>
                  {name(String(d.recipient_id))} ·{" "}
                  {d.connection_id ? "External provider" : "Native inbox"}
                </small>
              </span>
              <Badge
                tone={
                  ["received", "provider_accepted"].includes(String(d.state))
                    ? "green"
                    : d.state === "unknown"
                      ? "amber"
                      : "muted"
                }
              >
                {String(d.state)}
              </Badge>
              <span className="small muted">{String(d.attempts)} attempts</span>
              <span className="small">{String(d.last_error ?? "")}</span>
              {["failed", "unknown"].includes(String(d.state)) && (
                <button
                  className="secondary"
                  onClick={() => {
                    if (
                      d.state === "unknown" &&
                      !confirm(
                        "The provider may already have delivered this. Retry with a possible duplicate?",
                      )
                    )
                      return;
                    void act(
                      () => api("/deliveries/" + d.id + "/retry", {}),
                      "Queued for retry",
                    );
                  }}
                >
                  Retry
                </button>
              )}
            </div>
          ))}
          {deliveries.length === 0 && (
            <p className="muted pad">No deliveries yet.</p>
          )}
        </div>
      )}
    </>
  );
}
function Settings({
  me,
  people,
  act,
}: {
  me: Me;
  people: Entity[];
  act: FormsProps["act"];
}) {
  const [credentials, setCredentials] = useState<Record<string, unknown>[]>([]),
    [principal, setPrincipal] = useState(me.person.id),
    [kind, setKind] = useState("api"),
    [prefix, setPrefix] = useState("research/"),
    [issued, setIssued] = useState(""),
    [serviceName, setServiceName] = useState("");
  const load = () =>
    api<Record<string, unknown>[]>("/credentials").then(setCredentials);
  useEffect(() => {
    void load();
  }, []);
  return (
    <>
      <SharedAnswers />
      <section className="settings-section">
        <h2>Software services</h2>
        <p>
          Give applications and trusted agent launchers their own identity,
          separate from people and agents.
        </p>
        <form
          className="token-form"
          onSubmit={(e) => {
            e.preventDefault();
            void act(async () => {
              await api("/services", {
                name: serviceName,
                external_id: serviceName
                  .toLowerCase()
                  .replace(/[^a-z0-9]+/g, "-"),
              });
              setServiceName("");
            }, "Service created");
          }}
        >
          <Field label="Service name">
            <input
              value={serviceName}
              onChange={(e) => setServiceName(e.target.value)}
              required
            />
          </Field>
          <button className="secondary">Add service</button>
        </form>
        {people
          .filter((p) => p.kind === "service")
          .map((p) => (
            <p key={p.id}>
              <strong>{p.name}</strong> <code>{p.external_id}</code>
            </p>
          ))}
      </section>
      <section className="settings-section">
        <h2>Workspace</h2>
        <dl>
          <dt>Company</dt>
          <dd>{me.company}</dd>
          <dt>Environment</dt>
          <dd>{me.environment}</dd>
          <dt>Deployment</dt>
          <dd>Self-hosted · PostgreSQL</dd>
        </dl>
      </section>
      <section className="settings-section">
        <h2>Access tokens</h2>
        <p>
          Use independent credentials for applications and launchers. Enrollment
          tokens are restricted to a namespace; workers receive their own
          credentials.
        </p>
        <form
          className="token-form"
          onSubmit={(e) => {
            e.preventDefault();
            void act(async () => {
              const x = await api<{ token: string }>("/credentials", {
                principal_id: principal,
                kind,
                prefix: kind === "enrollment" ? prefix : "",
              });
              setIssued(x.token);
              await load();
            }, "Credential created");
          }}
        >
          <Field label="Issued to">
            <select
              value={principal}
              onChange={(e) => setPrincipal(e.target.value)}
            >
              {people.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} · {p.kind}
                </option>
              ))}
            </select>
          </Field>
          <Field label="Purpose">
            <select value={kind} onChange={(e) => setKind(e.target.value)}>
              <option value="api">API access</option>
              <option value="enrollment">Agent enrollment</option>
            </select>
          </Field>
          {kind === "enrollment" && (
            <Field label="Allowed namespace">
              <input
                value={prefix}
                onChange={(e) => setPrefix(e.target.value)}
                required
              />
            </Field>
          )}
          <button className="primary">Issue token</button>
        </form>
        {issued && (
          <div className="issued-secret">
            <strong>Copy this token now. It is shown once.</strong>
            <code>{issued}</code>
            <button
              className="secondary"
              onClick={() => void navigator.clipboard.writeText(issued)}
            >
              Copy token
            </button>
            <button className="quiet" onClick={() => setIssued("")}>
              Dismiss
            </button>
          </div>
        )}
        <div className="simple-list">
          {credentials.map((c) => (
            <div className="credential-row" key={String(c.id)}>
              <span>
                <strong>{String(c.kind)}</strong>
                <small>
                  {people.find((p) => p.id === c.principal_id)?.name ??
                    String(c.principal_id)}
                  {c.prefix ? ` · ${String(c.prefix)}` : ""}
                </small>
              </span>
              <Badge tone={c.revoked ? "muted" : "green"}>
                {c.revoked ? "Revoked" : "Active"}
              </Badge>
              {!c.revoked && (
                <button
                  className="quiet"
                  onClick={() =>
                    void act(async () => {
                      await api("/credentials/" + c.id + "/revoke", {});
                      await load();
                    }, "Credential revoked")
                  }
                >
                  Revoke
                </button>
              )}
            </div>
          ))}
        </div>
      </section>
    </>
  );
}
