import { useDeferredValue, useState, useEffect } from "react";
import {
  useCapabilities,
  useCollection,
  useOperation,
  usePostResource,
  useSession,
} from "../data";
import type {
  Identity,
  Instance,
  Target,
  FleetSource,
  Group,
  Message,
} from "../contracts";
import { navigate, useRoute } from "../navigation";
import { Compose } from "../Compose";
import {
  Button,
  Copy,
  Details,
  ErrorView,
  Field,
  Form,
  IdentityIcon,
  Icon,
  Modal,
  PageHeader,
  QueryState,
  Search,
  Status,
  Tabs,
} from "../ui";
import { OverseerEntry } from "../OverseerEntry";
import Groups from "./Groups";
import type { AgentDeadlock } from "./Fleet";
import AgentProfile from "./AgentProfile";
import { AgentSetup, connectionMethods } from "../AgentSetup";
export default function Directory({
  section,
  onAddPerson,
}: {
  section: string;
  onAddPerson?: () => void;
}) {
  const agents = section === "agents";
  const route = useRoute();
  const [search, setSearch] = useState(""),
    [tab, setTab] = useState("directory"),
    [selected, setSelected] = useState<Identity | null>(null),
    [compose, setCompose] = useState<Target[] | null>(null),
    [composeMode, setComposeMode] = useState("message"),
    [creating, setCreating] = useState(false),
    [connect, setConnect] = useState(false),
    [role, setRole] = useState(""),
    [presence, setPresence] = useState("");
  const q = useCollection<Identity>(
      agents ? "agents" : "people",
      useDeferredValue(search),
      agents
        ? {
            ...(role ? { group_id: role } : {}),
            ...(presence ? { presence } : {}),
          }
        : {},
    ),
    caps = useCapabilities();
  const roles = useCollection<Group>(
    "groups",
    "",
    { purpose: "agent_role" },
    agents,
  );
  const linkedPersonID = !agents
    ? route.id || new URLSearchParams(location.search).get("agent") || ""
    : "";
  const linkedPerson = useCollection<Identity>(
    "people",
    "",
    { id: linkedPersonID },
    !!linkedPersonID,
  );
  const fleet = usePostResource<{
    agents: FleetSource[];
    deadlocks: AgentDeadlock[];
  }>("/fleet/status", {}, agents);
  const deadlocks = agents ? fleet.data?.deadlocks || [] : [];
  useEffect(() => {
    const id = new URLSearchParams(location.search).get("agent");
    const groupID = new URLSearchParams(location.search).get("role");
    if (agents && groupID) {
      setTab("groups");
    }
    if (agents && id) navigate("/agents/" + encodeURIComponent(id));
  }, [agents, route.id]);
  useEffect(() => {
    if (linkedPerson.items[0]) setSelected(linkedPerson.items[0]);
  }, [linkedPerson.data]);
  if (agents && route.id)
    return (
      <>
        <AgentProfile key={route.id} id={route.id} onConnect={setSelected} />
        {selected ? (
          <Connect agent={selected} onClose={() => setSelected(null)} />
        ) : null}
      </>
    );
  return (
    <>
      <PageHeader
        title={agents ? "Agents" : "People"}
        actions={
          agents ? (
            caps.data?.operations.enroll_agent ? (
              <Button primary icon="plus" onClick={() => setConnect(true)}>
                Connect Agent
              </Button>
            ) : null
          ) : caps.data?.operations.create_person ? (
            <Button
              primary
              icon="plus"
              onClick={() => (onAddPerson ? onAddPerson() : setCreating(true))}
            >
              {onAddPerson ? "Invite people" : "Add person"}
            </Button>
          ) : null
        }
      />
      {deadlocks.length ? (
        <button
          className="dependency-notice"
          onClick={() => navigate("/fleet")}
        >
          <Icon name="warning" size={16} />
          <strong>Agents are blocked in a dependency cycle</strong>
          <span>Review</span>
          <Icon name="arrow" size={15} />
        </button>
      ) : null}
      <div className="directory-toolbar">
        <div className="directory-tabs">
          <button
            aria-pressed={tab === "directory"}
            onClick={() => setTab("directory")}
          >
            {agents ? "Agents" : "People"}
          </button>
          <button
            aria-pressed={tab === "groups"}
            onClick={() => setTab("groups")}
          >
            {agents ? "Agent roles" : "Teams"}
          </button>
        </div>
        {tab === "directory" ? (
          <div className="directory-filters">
            {agents ? (
              <>
                <select
                  aria-label="Filter by Agent role"
                  value={role}
                  onChange={(e) => setRole(e.target.value)}
                >
                  <option value="">All roles</option>
                  {roles.items.map((g) => (
                    <option key={g.id} value={g.id}>
                      {g.name}
                    </option>
                  ))}
                </select>
                <select
                  aria-label="Filter by connection"
                  value={presence}
                  onChange={(e) => setPresence(e.target.value)}
                >
                  <option value="">All connections</option>
                  <option value="connected">Connected</option>
                  <option value="stale">Stale</option>
                  <option value="offline">Offline</option>
                </select>
              </>
            ) : null}
            <Search
              value={search}
              onChange={setSearch}
              placeholder={agents ? "Search Agents" : "Search people"}
            />
          </div>
        ) : null}
      </div>
      {tab === "groups" ? (
        <Groups
          purpose={agents ? "agent_role" : "team"}
          initialID={
            new URLSearchParams(location.search).get("role") || undefined
          }
          embedded
        />
      ) : (
        <>
          {agents && !search ? <OverseerEntry /> : null}
          <QueryState
            query={q}
            empty={
              search
                ? "No matches"
                : agents
                  ? "Connect your first Agent"
                  : "No people yet"
            }
          >
            <div className="contact-directory">
              {q.items.map((item) => (
                <button
                  className="contact-row"
                  key={item.id}
                  onClick={() =>
                    agents ? navigate("/agents/" + item.id) : setSelected(item)
                  }
                >
                  <IdentityIcon
                    id={item.id}
                    kind={item.kind}
                    label={item.name}
                    icon={String(item.metadata?.appearance_icon || "")}
                  />
                  <div className="contact-name">
                    <strong>{item.name}</strong>
                    <small>
                      {String(
                        item.metadata?.[agents ? "description" : "email"] ||
                          item.metadata?.[agents ? "type" : "department"] ||
                          "",
                      )}
                    </small>
                  </div>
                  <Status
                    value={
                      item.disabled
                        ? "disabled"
                        : agents
                          ? fleet.data?.agents
                              ?.find((a) => a.agent.id === item.id)
                              ?.instances?.some(
                                (i) => i.presence === "connected",
                              )
                            ? "connected"
                            : fleet.data
                              ? "offline"
                              : "unknown"
                          : "enabled"
                    }
                  />
                </button>
              ))}
            </div>
          </QueryState>
        </>
      )}
      {selected ? (
        <IdentityDetail
          identity={selected}
          onClose={() => {
            setSelected(null);
            if (linkedPersonID) navigate("/people");
          }}
          onCompose={(t, mode = "message") => {
            setCompose([t]);
            setComposeMode(mode);
            setSelected(null);
          }}
        />
      ) : null}
      {compose ? (
        <Compose
          initial={compose}
          mode={composeMode}
          onClose={() => setCompose(null)}
        />
      ) : null}
      {creating ? <PersonForm onClose={() => setCreating(false)} /> : null}
      {connect ? <Connect onClose={() => setConnect(false)} /> : null}
    </>
  );
}
export function IdentityDetail({
  identity: p,
  onClose,
  onCompose,
}: {
  identity: Identity;
  onClose: () => void;
  onCompose: (t: Target, mode?: string) => void;
}) {
  const caps = useCapabilities({ kind: p.kind, id: p.id }),
    op = useOperation(),
    me = useSession();
  const instances = useCollection<Instance>(
    "instances",
    "",
    { agent_id: p.id },
    p.kind === "agent" && !!caps.data?.operations.view,
  );
  const [editing, setEditing] = useState(false),
    [confirm, setConfirm] = useState(false),
    [connect, setConnect] = useState(false);
  return (
    <Modal title={p.name} onClose={onClose}>
      <div className="contact-profile">
        <IdentityIcon
          kind={p.kind}
          id={p.id}
          label={p.name}
          icon={String(p.metadata?.appearance_icon || "")}
        />
        <div>
          <h2>{p.name}</h2>
          <p>
            {p.kind === "person"
              ? String(p.metadata?.email || "Person")
              : p.kind === "service"
                ? "Software service"
                : "Agent"}
          </p>
        </div>
        <Status value={p.disabled ? "disabled" : "enabled"} />
      </div>
      <ErrorView error={caps.error || op.error} />
      <div className="actions">
        {caps.data?.operations.message ? (
          <Button
            primary
            icon="conversations"
            onClick={() => onCompose({ kind: p.kind, id: p.id })}
          >
            Message
          </Button>
        ) : null}
        {caps.data?.operations.ask && !caps.data?.operations.message ? (
          <Button
            onClick={() => onCompose({ kind: p.kind, id: p.id }, "question")}
          >
            Request a reply
          </Button>
        ) : null}
        <Button
          onClick={() => {
            onClose();
            navigate("/permissions/" + p.id);
          }}
        >
          Permissions
        </Button>
        {p.kind === "person" && caps.data?.operations.manage ? (
          <Button onClick={() => setEditing(true)}>Edit profile</Button>
        ) : null}
      </div>
      {p.kind === "agent" ? (
        <section className="section">
          <div className="actions">
            <h3>Connections</h3>
            {me.admin ? (
              <Button icon="plus" onClick={() => setConnect(true)}>
                Add connection
              </Button>
            ) : null}
          </div>
          <QueryState query={instances} empty="No connections yet">
            {instances.items.map((instance, i) => (
              <div className="instance-row" key={instance.id}>
                <div>
                  <strong>Connection {i + 1}</strong>
                  <small>
                    Last seen {new Date(instance.last_seen).toLocaleString()}
                  </small>
                </div>
                <Status value={instance.presence} />
                {caps.data?.operations.message ? (
                  <Button
                    onClick={() =>
                      onCompose({ kind: "instance", id: instance.id })
                    }
                  >
                    Message
                  </Button>
                ) : null}
              </div>
            ))}
          </QueryState>
        </section>
      ) : null}
      <details className="advanced-access">
        <summary>Details</summary>
        <Details
          label="Identifiers"
          value={{ id: p.id, external_id: p.external_id, metadata: p.metadata }}
        />
        {caps.data?.operations.manage && p.id !== me.person.id ? (
          <Button danger={!p.disabled} onClick={() => setConfirm(true)}>
            {p.disabled ? "Enable access" : "Disable access"}
          </Button>
        ) : null}
      </details>
      {confirm ? (
        <div className="approval">
          <p>
            {p.disabled ? "Restore" : "Disable"} Beepgrid access for {p.name}?
          </p>
          <div className="actions">
            <Button onClick={() => setConfirm(false)}>Cancel</Button>
            <Button
              primary
              disabled={op.pending}
              onClick={() =>
                void op
                  .run(
                    `/${p.kind === "person" ? "people" : p.kind === "agent" ? "agents" : "services"}/${p.id}/status`,
                    { disabled: !p.disabled },
                  )
                  .then((r) => {
                    if (r) onClose();
                  })
              }
            >
              Confirm
            </Button>
          </div>
        </div>
      ) : null}
      {editing ? (
        <PersonForm
          person={p}
          onClose={() => setEditing(false)}
          onSaved={onClose}
        />
      ) : null}
      {connect ? <Connect agent={p} onClose={() => setConnect(false)} /> : null}
    </Modal>
  );
}
export function PersonForm({
  person,
  onClose,
  onSaved,
}: {
  person?: Identity;
  onClose: () => void;
  onSaved?: () => void;
}) {
  const [name, setName] = useState(person?.name || ""),
    [email, setEmail] = useState(String(person?.metadata?.email || "")),
    [department, setDepartment] = useState(
      String(person?.metadata?.department || ""),
    ),
    [more, setMore] = useState(false);
  const [external] = useState(() => "person/" + crypto.randomUUID());
  const op = useOperation();
  return (
    <Modal title={person ? "Edit person" : "Add person"} onClose={onClose}>
      <Form
        onSubmit={() =>
          void op
            .run(person ? `/people/${person.id}/profile` : "/people", {
              name,
              ...(!person ? { external_id: external } : {}),
              email,
              department,
              manager_id: person?.metadata?.manager_id || "",
            })
            .then((r) => {
              if (r) {
                onClose();
                onSaved?.();
              }
            })
        }
      >
        <Field label="Name">
          <input
            required
            maxLength={120}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>
        <Field label="Email">
          <input
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </Field>
        <Button onClick={() => setMore((x) => !x)}>Profile details</Button>
        {more ? (
          <Field label="Department">
            <input
              value={department}
              onChange={(e) => setDepartment(e.target.value)}
            />
          </Field>
        ) : null}
        <ErrorView error={op.error} />
        <div className="form-actions">
          <Button onClick={onClose}>Cancel</Button>
          <Button primary type="submit" disabled={op.pending}>
            Save person
          </Button>
        </div>
      </Form>
    </Modal>
  );
}
export function Connect({
  onClose,
  agent,
}: {
  onClose: () => void;
  agent?: Identity;
}) {
  const me = useSession(),
    op = useOperation();
  const [name, setName] = useState(agent?.name || ""),
    [external] = useState(
      () => agent?.external_id || "agent/" + crypto.randomUUID(),
    ),
    [session, setSession] = useState<{
      token: string;
      agent: Identity;
      instance_id: string;
    } | null>(null),
    [contact, setContact] = useState(true),
    [runtime, setRuntime] = useState("python"),
    [testConversation, setTestConversation] = useState("");
  const base = location.origin;
  const replies = useCollection<Message>(
    "messages",
    "",
    { conversation_id: testConversation },
    !!testConversation,
  );
  const replied =
    !!session &&
    replies.items.some(
      (m) =>
        m.sender_id === session.agent.id &&
        m.instance_id === session.instance_id,
    );
  async function prepare() {
    const result = await op.run<{
      token: string;
      agent: Identity;
      instance_id: string;
    }>("/sessions", {
      agent: external,
      name,
      capabilities: ["message", "status"],
    });
    if (!result) return;
    setSession(result);
    if (contact && !agent)
      await op.run("/contact-permissions", {
        subject: { kind: "agent", id: result.agent.id },
        rules: ["view", "message", "ask", "decide"].map((action) => ({
          action,
          target_kind: "person",
          target_id: me.person.id,
        })),
        version: 0,
      });
  }
  return (
    <Modal
      title={agent ? `Connect ${agent.name}` : "Connect an Agent"}
      onClose={onClose}
      className="agent-connect-dialog"
    >
      {session ? (
        <div className="form">
          <div className="setup-agent">
            <IdentityIcon
              kind="agent"
              id={session.agent.id}
              label={session.agent.name}
            />
            <strong>{session.agent.name}</strong>
          </div>
          <AgentSetup
            method={runtime}
            onMethodChange={setRuntime}
            base={base}
            token={session.token}
          />
          <section className="setup-step setup-verification">
            <header>
              <span>3</span>
              <h3>Verify your connection</h3>
            </header>
            {testConversation ? (
              <div className="setup-verify-state" role="status">
                <Status value={replied ? "connected" : "waiting"} />
                <p>
                  {replied
                    ? "Reply received from " + session.agent.name + "."
                    : "Waiting for your worker’s reply..."}
                </p>
                <Button
                  onClick={() => {
                    onClose();
                    navigate("/conversations/" + testConversation);
                  }}
                >
                  Open test conversation
                </Button>
              </div>
            ) : (
              <Button
                disabled={op.pending}
                onClick={() =>
                  void op
                    .run<{ conversation_id: string }>("/messages", {
                      target: { kind: "instance", id: session.instance_id },
                      body: "Beepgrid connection check. Please reply to this message.",
                    })
                    .then((r) => {
                      if (r) setTestConversation(r.conversation_id);
                    })
                }
              >
                Send a test message
              </Button>
            )}
            <ErrorView error={replies.error} />
            <ErrorView error={op.error} />
          </section>
          <div className="form-actions">
            <Button
              onClick={() => {
                onClose();
                navigate("/permissions/" + session.agent.id);
              }}
            >
              Manage permissions
            </Button>
            <Button primary onClick={onClose}>
              Done
            </Button>
          </div>
        </div>
      ) : (
        <Form onSubmit={() => void prepare()}>
          <Field label="Agent name">
            <input
              required
              placeholder="Builder 1"
              maxLength={120}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </Field>
          <div className="field" role="group" aria-label="Connection method">
            <span>Connection method</span>
            <Tabs
              value={runtime}
              onChange={setRuntime}
              options={connectionMethods}
            />
          </div>
          {!agent ? (
            <label className="inline-check">
              <input
                type="checkbox"
                checked={contact}
                onChange={(e) => setContact(e.target.checked)}
              />
              Allow this Agent to contact me
            </label>
          ) : null}
          <ErrorView error={op.error} />
          <div className="form-actions">
            <Button onClick={onClose}>Cancel</Button>
            <Button primary type="submit" disabled={op.pending || !name.trim()}>
              Create connection
            </Button>
          </div>
        </Form>
      )}
    </Modal>
  );
}
