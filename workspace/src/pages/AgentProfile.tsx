import { lazy, Suspense, useState } from "react";
import {
  useCapabilities,
  useCollection,
  useOperation,
  usePostResource,
} from "../data";
import type {
  Conversation,
  Destination,
  Connection,
  Group,
  Identity,
  Instance,
  Request,
  Target,
} from "../contracts";
import { navigate } from "../navigation";
import { Compose } from "../Compose";
import {
  Badge,
  Button,
  Details,
  ErrorView,
  Icon,
  IdentityIcon,
  Loading,
  Modal,
  QueryState,
  Status,
  Tabs,
} from "../ui";
import { GroupForm } from "./Groups";

const Permissions = lazy(() => import("./Permissions"));
const AgentContacts = lazy(() => import("./AgentContacts"));
const providerNames: Record<string, string> = {
  slack: "Slack",
  email: "Email",
  webhook: "Webhook",
  telegram: "Telegram",
  discord: "Discord",
  native: "Beepgrid",
};

// Exact lookup does not depend on the currently loaded directory page. Every
// section uses the existing independently authorized collections and actions.
export default function AgentProfile({
  id,
  onConnect,
}: {
  id: string;
  onConnect: (p: Identity) => void;
}) {
  const identity = useCollection<Identity>("agents", "", { id });
  const p = identity.items[0];
  return (
    <>
      <div className="profile-breadcrumb">
        <Button icon="chevron" onClick={() => navigate("/agents")}>
          Agents
        </Button>
        <span>/</span>
        <span>{p?.name || "Agent"}</span>
      </div>
      <QueryState query={identity} empty="This agent is unavailable.">
        {p ? <Profile key={p.id} agent={p} onConnect={onConnect} /> : null}
      </QueryState>
    </>
  );
}

function Profile({
  agent: p,
  onConnect,
}: {
  agent: Identity;
  onConnect: (p: Identity) => void;
}) {
  const [tab, setTab] = useState("overview"),
    [compose, setCompose] = useState<Target[] | null>(null),
    [editRoles, setEditRoles] = useState(false),
    [confirm, setConfirm] = useState(false);
  const caps = useCapabilities({ kind: "agent", id: p.id });
  const op = useOperation();
  const instances = useCollection<Instance>("instances", "", {
    agent_id: p.id,
  });
  const roles = useCollection<Group>("groups", "", {
    purpose: "agent_role",
    member_id: p.id,
  });
  const history = useCollection<Conversation>("conversations", "", {
    participant_id: p.id,
  });
  const sent = useCollection<Request>("requests", "", {
    requester_id: p.id,
    state: "open",
  });
  const received = useCollection<Request>("requests", "", {
    recipient_id: p.id,
    state: "open",
  });
  const global = useCapabilities();
  const connected = instances.items.filter((i) => i.presence === "connected");
  const state = p.disabled
    ? "disabled"
    : connected.length
      ? "connected"
      : instances.isPending
        ? "unknown"
        : instances.items.some((i) => i.presence === "stale")
          ? "stale"
          : "offline";
  const waiting = [
    ...new Map(
      [...sent.items, ...received.items].map((r) => [r.id, r]),
    ).values(),
  ];
  const description = String(p.metadata?.description || "");
  const canMessage = !!caps.data?.operations.message;
  return (
    <div className="agent-profile">
      <header className="agent-profile-head">
        <IdentityIcon kind="agent" id={p.id} label={p.name} />
        <div className="agent-profile-title">
          <div>
            <h1>{p.name}</h1>
            <Status value={state} />
          </div>
          {description ? <p>{description}</p> : null}
        </div>
        <div className="actions">
          {caps.data?.operations.enroll_agent ||
          global.data?.operations.enroll_agent ? (
            <Button icon="plus" onClick={() => onConnect(p)}>
              Add connection
            </Button>
          ) : null}
          {canMessage ? (
            <Button
              primary
              icon="conversations"
              onClick={() => setCompose([{ kind: "agent", id: p.id }])}
            >
              Message
            </Button>
          ) : null}
        </div>
      </header>
      <div className="agent-role-line">
        <Icon name="groups" size={15} />
        <span>Agent roles</span>
        {roles.items.map((g) => (
          <Badge key={g.id}>{g.name}</Badge>
        ))}
        {!roles.isPending && !roles.items.length ? (
          <span className="muted">No visible roles</span>
        ) : null}
        {global.data?.operations.create_group ? (
          <button className="text-action" onClick={() => setEditRoles(true)}>
            Manage
          </button>
        ) : null}
      </div>
      <ErrorView error={caps.error || roles.error || op.error} />
      <Tabs
        value={tab}
        onChange={setTab}
        options={[
          { id: "overview", label: "Overview" },
          { id: "conversations", label: "Conversations" },
          { id: "channels", label: "Channels" },
          { id: "access", label: "Access" },
        ]}
      />
      {tab === "overview" ? (
        <div className="agent-overview">
          <div>
            <section className="profile-section">
              <header>
                <h2>Pending replies</h2>
                {waiting.length ? (
                  <Badge>
                    {waiting.length}
                    {sent.hasNextPage || received.hasNextPage ? "+" : ""}
                  </Badge>
                ) : null}
              </header>
              <ErrorView error={sent.error || received.error} />
              {sent.isPending || received.isPending ? (
                <Loading />
              ) : waiting.length ? (
                <div className="profile-pending">
                  {waiting.slice(0, 5).map((r) => (
                    <button
                      key={r.id}
                      onClick={() =>
                        navigate("/conversations/" + r.conversation_id)
                      }
                    >
                      <span className="pending-symbol">
                        <Icon
                          name={
                            r.kind === "approval" ? "check" : "conversations"
                          }
                          size={17}
                        />
                      </span>
                      <div>
                        <span className="profile-request-label">
                          {r.requester_id === p.id
                            ? "Waiting on a response"
                            : "Waiting on " + p.name}
                          {r.blocking ? " · Blocking" : ""}
                        </span>
                        <p>{r.body || r.title}</p>
                      </div>
                      <Icon name="chevron" size={15} />
                    </button>
                  ))}
                  {waiting.length > 5 ? (
                    <Button onClick={() => navigate("/fleet")}>
                      View all pending communication
                    </Button>
                  ) : null}
                  {sent.hasNextPage || received.hasNextPage ? (
                    <Button
                      disabled={
                        sent.isFetchingNextPage || received.isFetchingNextPage
                      }
                      onClick={() => {
                        if (sent.hasNextPage) void sent.fetchNextPage();
                        if (received.hasNextPage) void received.fetchNextPage();
                      }}
                    >
                      Load more pending replies
                    </Button>
                  ) : null}
                </div>
              ) : (
                <div className="profile-inline-empty">
                  <Icon name="check" size={19} />
                  <span>No pending replies</span>
                </div>
              )}
            </section>
            <section className="profile-section">
              <header>
                <h2>Recent conversations</h2>
                <button
                  className="text-action"
                  onClick={() => setTab("conversations")}
                >
                  View all
                  <Icon name="arrow" size={13} />
                </button>
              </header>
              <ConversationList query={history} limit={4} agentID={p.id} />
            </section>
          </div>
          <aside>
            <section className="profile-section profile-runtime">
              <header>
                <h2>Runtime connections</h2>
                <Badge>
                  {instances.items.length}
                  {instances.hasNextPage ? "+" : ""}
                </Badge>
              </header>
              <QueryState query={instances} empty="No runtime connections">
                {instances.items.map((instance, i) => (
                  <div className="profile-instance" key={instance.id}>
                    <Icon name="service" size={19} />
                    <div>
                      <strong>Connection {i + 1}</strong>
                      <small>
                        Last seen{" "}
                        {new Date(instance.last_seen).toLocaleTimeString([], {
                          hour: "2-digit",
                          minute: "2-digit",
                        })}
                      </small>
                    </div>
                    <Status value={instance.presence} />
                    {canMessage && instances.items.length > 1 ? (
                      <button
                        className="text-action"
                        aria-label={`Message connection ${i + 1}`}
                        onClick={() =>
                          setCompose([{ kind: "instance", id: instance.id }])
                        }
                      >
                        <Icon name="conversations" size={15} />
                      </button>
                    ) : null}
                  </div>
                ))}
              </QueryState>
              {p.metadata?.runtime ? (
                <div className="profile-runtime-meta">
                  <span>Runtime</span>
                  <strong>{String(p.metadata.runtime)}</strong>
                </div>
              ) : null}
            </section>
            <section className="profile-section profile-shortcuts">
              <h2>Communication</h2>
              <button onClick={() => setTab("channels")}>
                <Icon name="integrations" size={17} />
                <span>Delivery channels</span>
                <Icon name="chevron" size={14} />
              </button>
              <button onClick={() => setTab("access")}>
                <Icon name="permissions" size={17} />
                <span>Contact permissions</span>
                <Icon name="chevron" size={14} />
              </button>
              <button onClick={() => navigate("/fleet")}>
                <Icon name="fleet" size={17} />
                <span>Fleet activity</span>
                <Icon name="chevron" size={14} />
              </button>
            </section>
          </aside>
        </div>
      ) : null}
      {tab === "conversations" ? (
        <section className="profile-section">
          <ConversationList query={history} agentID={p.id} />
        </section>
      ) : null}
      {tab === "channels" ? <ProfileChannels agent={p} /> : null}
      {tab === "access" ? (
        <Suspense fallback={<Loading />}>
          <Permissions profile={p} embedded />
        </Suspense>
      ) : null}
      <details className="advanced-access profile-details">
        <summary>Agent details</summary>
        <Details
          label="Identifiers"
          value={{ id: p.id, external_id: p.external_id, metadata: p.metadata }}
        />
        {caps.data?.operations.manage ? (
          <Button danger={!p.disabled} onClick={() => setConfirm(true)}>
            {p.disabled ? "Enable agent" : "Disable agent"}
          </Button>
        ) : null}
      </details>
      {confirm ? (
        <Modal
          title={p.disabled ? "Enable agent" : "Disable agent"}
          onClose={() => setConfirm(false)}
        >
          <p>
            {p.disabled ? "Restore" : "Disable"} communication access for{" "}
            {p.name}?
          </p>
          <div className="form-actions">
            <Button onClick={() => setConfirm(false)}>Cancel</Button>
            <Button
              primary
              disabled={op.pending}
              onClick={() =>
                void op
                  .run(`/agents/${p.id}/status`, { disabled: !p.disabled })
                  .then((r) => {
                    if (r) setConfirm(false);
                  })
              }
            >
              Confirm
            </Button>
          </div>
        </Modal>
      ) : null}
      {compose ? (
        <Compose initial={compose} onClose={() => setCompose(null)} />
      ) : null}
      {editRoles ? (
        <AgentRoles agent={p} onClose={() => setEditRoles(false)} />
      ) : null}
    </div>
  );
}

function ConversationList({
  query: q,
  limit,
  agentID,
}: {
  query: ReturnType<typeof useCollection<Conversation>>;
  limit?: number;
  agentID: string;
}) {
  return (
    <QueryState query={q} empty="No conversations you can access yet.">
      <div className="profile-conversations">
        {q.items.slice(0, limit).map((c) => {
          const peers = c.participants.filter((p) => p.id !== agentID);
          return (
            <button
              key={c.id}
              onClick={() => navigate("/conversations/" + c.id)}
            >
              <IdentityIcon
                kind={peers.length > 1 ? "group" : peers[0]?.kind || "person"}
                id={c.id}
                label={c.title}
              />
              <div>
                <strong>
                  {peers.length > 1 ? c.title : peers[0]?.name || c.title}
                </strong>
                <p>{c.last_preview || "Open conversation"}</p>
              </div>
              <time>
                {new Date(
                  c.last_activity_at || c.created_at,
                ).toLocaleDateString([], { month: "short", day: "numeric" })}
              </time>
              <Icon name="chevron" size={14} />
            </button>
          );
        })}
      </div>
    </QueryState>
  );
}

function AgentRoles({
  agent,
  onClose,
}: {
  agent: Identity;
  onClose: () => void;
}) {
  const groups = useCollection<Group>("groups", "", { purpose: "agent_role" });
  const [edit, setEdit] = useState<Group | null>(null);
  return (
    <Modal title={`Agent roles for ${agent.name}`} onClose={onClose}>
      <QueryState query={groups} empty="No agent roles yet.">
        <div className="profile-role-list">
          {groups.items.map((g) => (
            <RoleMembership
              key={g.id}
              group={g}
              agent={agent}
              onEdit={() => setEdit(g)}
            />
          ))}
        </div>
      </QueryState>
      {edit ? (
        <GroupForm
          group={edit}
          purpose="agent_role"
          onClose={() => setEdit(null)}
        />
      ) : null}
    </Modal>
  );
}

function RoleMembership({
  group: g,
  agent,
  onEdit,
}: {
  group: Group;
  agent: Identity;
  onEdit: () => void;
}) {
  const caps = useCapabilities({ kind: "group", id: g.id });
  return (
    <div>
      <IdentityIcon kind="group" id={g.id} />
      <strong>{g.name}</strong>
      {g.members.includes(agent.id) ? <Badge tone="good">Member</Badge> : null}
      {caps.data?.operations.manage ? (
        <Button onClick={onEdit}>Edit members</Button>
      ) : null}
    </div>
  );
}

function ProfileChannels({ agent }: { agent: Identity }) {
  const destinations = useCollection<Destination>("destinations", "", {
    principal_id: agent.id,
  });
  const connections = useCollection<Connection>("connections");
  const preview = usePostResource<{
    recipients: {
      routes: {
        kind: string;
        mode: string;
        enabled: boolean;
        permitted: boolean;
      }[];
    }[];
  }>("/dispatch-preview", {
    operation: "message",
    targets: [{ kind: "agent", id: agent.id }],
  });
  const caps = useCapabilities({ kind: "agent", id: agent.id });
  const native = preview.data?.recipients[0]?.routes.find(
    (r) => r.kind === "native",
  );
  return (
    <>
      <Suspense fallback={<Loading />}>
        <AgentContacts agent={agent} />
      </Suspense>
      <section className="profile-section profile-channels">
        <header>
          <h2>Receiving messages</h2>
          <Button icon="integrations" onClick={() => navigate("/integrations")}>
            Manage integrations
          </Button>
        </header>
        <ErrorView
          error={
            destinations.error ||
            connections.error ||
            (caps.data?.operations.message ? preview.error : null)
          }
        />
        <div className="profile-channel-row">
          <Icon name="agents" size={21} />
          <div>
            <strong>Agent runtime</strong>
            <p>
              Messages are saved in Beepgrid and received by this agent’s
              runtime.
            </p>
          </div>
          <Badge>{native?.permitted ? "Available" : "Beepgrid"}</Badge>
        </div>
        {destinations.items.map((d) => {
          const c = connections.items.find((c) => c.id === d.connection_id);
          return (
            <div className="profile-channel-row" key={d.id}>
              <Icon
                name={
                  c?.kind === "email"
                    ? "mail"
                    : c?.kind === "slack"
                      ? "slack"
                      : "integrations"
                }
                size={21}
              />
              <div>
                <strong>{c?.name || "Delivery channel"}</strong>
                <p>
                  {providerNames[c?.kind || ""] || "External channel"} ·{" "}
                  {d.mode}
                </p>
              </div>
              <Badge>
                {c ? (c.enabled ? "Configured" : "Disabled") : "Restricted"}
              </Badge>
            </div>
          );
        })}
        {!destinations.isPending && !destinations.items.length ? (
          <p className="help">
            No additional delivery channels are visible for this agent.
          </p>
        ) : null}
      </section>
    </>
  );
}
