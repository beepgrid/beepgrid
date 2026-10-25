import { useDeferredValue, useState } from "react";
import {
  useCapabilities,
  useCollection,
  useOperation,
  useResource,
  useSession,
} from "../data";
import type { Group, Identity, Instance, Target } from "../contracts";
import { relative } from "../client";
import { navigate } from "../navigation";
import { Compose } from "../Compose";
import {
  Badge,
  Button,
  DateValue,
  Details,
  ErrorView,
  Field,
  Form,
  Icon,
  IdentityIcon,
  Modal,
  PageHeader,
  QueryState,
  Search,
  Status,
} from "../ui";
import Fleet from "./Fleet";

export default function Directory({ section }: { section: string }) {
  const agents = section === "agents";
  const [search, setSearch] = useState(""),
    [filter, setFilter] = useState(""),
    [presence, setPresence] = useState(""),
    [field, setField] = useState("project"),
    [value, setValue] = useState(""),
    [selected, setSelected] = useState<Identity | null>(null),
    [compose, setCompose] = useState<Target[] | null>(null),
    [composeMode, setComposeMode] = useState("message"),
    [creating, setCreating] = useState(false),
    [connect, setConnect] = useState(false),
    [selection, setSelection] = useState<string[]>([]),
    [fleet, setFleet] = useState(false);
  const filters: Record<string, string> = {};
  if (filter) filters.disabled = filter;
  if (agents && presence) filters.presence = presence;
  if (value) filters[field] = value;
  const q = useCollection<Identity>(
      agents ? "agents" : "people",
      useDeferredValue(search),
      filters,
    ),
    caps = useCapabilities();
  return (
    <>
      <PageHeader
        title={agents ? "Agents" : "People"}
        description={
          agents
            ? "Connected identities and their running instances."
            : "People in your permitted workspace."
        }
        actions={
          <>
            {agents ? (
              <>
                <Button icon="activity" onClick={() => setFleet(true)}>
                  Fleet status
                </Button>
                {caps.data?.operations.enroll_agent ? (
                  <Button primary icon="plus" onClick={() => setConnect(true)}>
                    Connect agent
                  </Button>
                ) : null}
              </>
            ) : caps.data?.operations.create_person ? (
              <Button primary icon="plus" onClick={() => setCreating(true)}>
                Add person
              </Button>
            ) : null}
          </>
        }
      />
      <div className="toolbar">
        <div className="filters">
          <select
            aria-label="Access status"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          >
            <option value="">Enabled and disabled</option>
            <option value="false">Enabled</option>
            <option value="true">Disabled</option>
          </select>
          {agents ? (
            <select
              aria-label="Instance presence"
              value={presence}
              onChange={(e) => setPresence(e.target.value)}
            >
              <option value="">Any instance presence</option>
              <option value="connected">Connected</option>
              <option value="stale">Stale</option>
              <option value="offline">Offline or no instances</option>
            </select>
          ) : null}
          <select
            aria-label="Filter field"
            value={field}
            onChange={(e) => setField(e.target.value)}
          >
            {(agents
              ? [
                  "project",
                  "type",
                  "group_id",
                  "role_id",
                  "enrolled_by",
                  "parent_id",
                ]
              : ["department", "manager_id", "group_id", "role_id"]
            ).map((f) => (
              <option key={f} value={f}>
                {f.replaceAll("_", " ")}
              </option>
            ))}
          </select>
          <input
            aria-label="Exact filter value"
            placeholder="Exact filter value"
            style={{ width: 160, minHeight: 32, fontSize: 11 }}
            value={value}
            onChange={(e) => setValue(e.target.value)}
          />
        </div>
        <Search
          value={search}
          onChange={setSearch}
          placeholder={agents ? "Search agents" : "Search people"}
        />
      </div>
      {selection.length ? (
        <div className="selection-bar">
          <span>{selection.length} selected</span>
          <div className="actions">
            <Button onClick={() => setSelection([])}>Clear</Button>
            <Button
              primary
              onClick={() =>
                setCompose(
                  selection.map((id) => ({
                    kind: agents ? "agent" : "person",
                    id,
                  })),
                )
              }
            >
              Communicate
            </Button>
          </div>
        </div>
      ) : null}
      <div className="collection-head">
        <span>{agents ? "Agent" : "Person"}</span>
        <span>Access</span>
        <span>{agents ? "Project" : "Department"}</span>
        <span>Created</span>
      </div>
      <QueryState
        query={q}
        empty={
          search || value || filter || presence
            ? "No matching identities"
            : "No permitted identities yet"
        }
      >
        {q.items.map((item) => (
          <div
            className="directory-row"
            key={item.id}
            onClick={() => setSelected(item)}
            role="button"
            tabIndex={0}
            onKeyDown={(e) => {
              if (
                e.target === e.currentTarget &&
                (e.key === "Enter" || e.key === " ")
              ) {
                e.preventDefault();
                setSelected(item);
              }
            }}
          >
            <div className="identity">
              <input
                type="checkbox"
                aria-label={`Select ${item.name}`}
                style={{ width: 13, minHeight: 13, margin: "0 2px 0 0" }}
                checked={selection.includes(item.id)}
                onClick={(e) => e.stopPropagation()}
                onChange={(e) =>
                  setSelection((x) =>
                    e.target.checked
                      ? [...x, item.id]
                      : x.filter((id) => id !== item.id),
                  )
                }
              />
              <IdentityIcon id={item.id} kind={item.kind} label={item.name} />
              <div>
                <strong>{item.name}</strong>
                <p>{String(item.metadata?.description || item.external_id)}</p>
              </div>
            </div>
            <div>
              <Status value={item.disabled ? "disabled" : "enabled"} />
            </div>
            <div>
              {String(
                item.metadata?.[agents ? "project" : "department"] || "Not set",
              )}
            </div>
            <div>
              <time title={item.created_at}>{relative(item.created_at)}</time>
            </div>
          </div>
        ))}
      </QueryState>
      <p className="collection-footer">
        {q.items.length} permitted {agents ? "agents" : "people"} loaded
        {q.hasNextPage ? " · More available" : ""}
      </p>
      {agents &&
      !search &&
      !value &&
      !filter &&
      !presence &&
      !q.isPending &&
      !q.error &&
      q.items.length === 0 ? (
        <section className="section">
          <h2>Make your first connection</h2>
          <p>
            Connect an agent through a trusted launcher, give it bounded contact
            rights, then start one conversation. Agents appear here
            automatically after enrollment.
          </p>
          {caps.data?.operations.enroll_agent ? (
            <div className="actions">
              <Button onClick={() => navigate("/people")}>Add people</Button>
              <Button onClick={() => navigate("/settings/services")}>
                Software services
              </Button>
              <Button onClick={() => navigate("/permissions")}>
                Set contact permissions
              </Button>
              <Button primary onClick={() => setConnect(true)}>
                Connect agent
              </Button>
            </div>
          ) : (
            <p className="help">
              Your workspace administrator or delegated launcher can connect
              agents. You can continue using the identities and conversations
              available to you.
            </p>
          )}
        </section>
      ) : null}
      {selected ? (
        <IdentityDetail
          identity={selected}
          onClose={() => setSelected(null)}
          onCompose={(t, mode = "message") => {
            setComposeMode(mode);
            setCompose([t]);
            setSelected(null);
          }}
        />
      ) : null}
      {compose ? (
        <Compose
          mode={composeMode}
          initial={compose}
          multiple={compose.length > 1}
          onClose={() => setCompose(null)}
        />
      ) : null}
      {creating ? <PersonForm onClose={() => setCreating(false)} /> : null}
      {connect ? <Connect onClose={() => setConnect(false)} /> : null}
      {fleet ? (
        <Modal title="Fleet" wide onClose={() => setFleet(false)}>
          <Fleet />
        </Modal>
      ) : null}
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
    p.kind === "agent" && caps.data?.operations.view === true,
  );
  const groups = useResource<Group[]>(
    "/groups",
    caps.data?.operations.view === true ||
      caps.data?.operations.manage === true,
  );
  const [editing, setEditing] = useState(false),
    [linking, setLinking] = useState(false),
    [confirm, setConfirm] = useState(false);
  if (caps.isPending || caps.error)
    return (
      <Modal title="Identity" onClose={onClose}>
        <ErrorView error={caps.error} />
        {caps.isPending ? <p>Checking current access.</p> : null}
      </Modal>
    );
  return (
    <Modal title={p.name} wide onClose={onClose}>
      <div className="intro">
        <IdentityIcon id={p.id} kind={p.kind} label={p.name} />
        <div>
          <h2>{p.name}</h2>
          <p>
            {p.kind === "service"
              ? "Software service"
              : p.kind === "agent"
                ? "Agent identity"
                : "Person"}{" "}
            · {p.external_id}
          </p>
        </div>
        <Badge>{p.disabled ? "Disabled" : "Enabled"}</Badge>
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
        {caps.data?.operations.ask ? (
          <Button
            onClick={() => onCompose({ kind: p.kind, id: p.id }, "question")}
          >
            Ask or request
          </Button>
        ) : null}
        <Button
          icon="permissions"
          onClick={() => {
            onClose();
            navigate("/permissions/" + p.id);
          }}
        >
          Access
        </Button>
        {p.kind === "person" && caps.data?.operations.manage ? (
          <Button onClick={() => setEditing(true)}>Edit profile</Button>
        ) : null}
        {caps.data?.operations.link_account ? (
          <Button onClick={() => setLinking(true)}>Link account</Button>
        ) : null}
        {caps.data?.operations.issue_credential ? (
          <Button
            onClick={() => {
              onClose();
              navigate("/settings/credentials");
            }}
          >
            Access tokens
          </Button>
        ) : null}
        {caps.data?.operations.manage && p.id !== me.person.id ? (
          <Button danger={!p.disabled} onClick={() => setConfirm(true)}>
            {p.disabled ? "Enable access" : "Disable access"}
          </Button>
        ) : null}
      </div>
      {confirm ? (
        <div className="approval">
          <p>
            Change Beep Grid access for {p.name}? Disabling revokes future reads
            and communication. It does not stop an external process or remove
            history.
          </p>
          <div className="form-actions">
            <Button onClick={() => setConfirm(false)}>Cancel</Button>
            <Button
              danger
              primary
              disabled={op.pending}
              onClick={() =>
                void op
                  .run(
                    "/" +
                      {
                        person: "people",
                        agent: "agents",
                        service: "services",
                      }[p.kind] +
                      "/" +
                      p.id +
                      "/status",
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
      <dl className="detail-list">
        <dt>Identity</dt>
        <dd>
          <code>{p.id}</code>
        </dd>
        <dt>Created</dt>
        <dd>
          <DateValue value={p.created_at} />
        </dd>
        {Object.entries(p.metadata || {})
          .filter(([, v]) => v !== "" && v !== null)
          .map(([k, v]) => (
            <div style={{ display: "contents" }} key={k}>
              <dt>{k.replaceAll("_", " ")}</dt>
              <dd>{typeof v === "object" ? JSON.stringify(v) : String(v)}</dd>
            </div>
          ))}
      </dl>
      <section className="section">
        <h3>Visible group memberships</h3>
        <ErrorView error={groups.error} />
        {groups.data ? (
          <>
            {groups.data
              .filter((g) => g.members.includes(p.id))
              .map((g) => (
                <p key={g.id}>
                  {g.name} <small>{g.id}</small>
                </p>
              ))}
            <p className="help">
              From your current permitted group list. Hidden groups and hidden
              roster entries are excluded. Role provenance is available under
              Access.
            </p>
          </>
        ) : null}
      </section>
      {p.kind === "agent" && caps.data?.operations.view ? (
        <section className="section">
          <h2>Running instances</h2>
          <p>Connection freshness and worker-reported progress are separate.</p>
          <QueryState query={instances} empty="No permitted instances">
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Instance</th>
                    <th>Presence</th>
                    <th>Last contact</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {instances.items.map((i) => (
                    <tr key={i.id}>
                      <td>
                        <code>{i.id}</code>
                        <Details
                          value={{
                            capabilities: i.capabilities,
                            progress: i.progress,
                          }}
                          label="Reported capabilities and progress"
                        />
                      </td>
                      <td>
                        <Status value={i.presence} />
                      </td>
                      <td>
                        <DateValue value={i.last_seen} />
                      </td>
                      <td>
                        {caps.data?.operations.message ||
                        caps.data?.operations.ask ? (
                          <Button
                            onClick={() =>
                              onCompose(
                                { kind: "instance", id: i.id },
                                caps.data?.operations.message
                                  ? "message"
                                  : "question",
                              )
                            }
                          >
                            Address instance
                          </Button>
                        ) : null}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </QueryState>
        </section>
      ) : null}
      {editing ? (
        <PersonForm
          person={p}
          onClose={() => setEditing(false)}
          onSaved={onClose}
        />
      ) : null}
      {linking ? (
        <LinkAccount person={p} onClose={() => setLinking(false)} />
      ) : null}
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
    [external, setExternal] = useState(""),
    [email, setEmail] = useState(String(person?.metadata?.email || "")),
    [department, setDepartment] = useState(
      String(person?.metadata?.department || ""),
    ),
    [manager, setManager] = useState(
      String(person?.metadata?.manager_id || ""),
    );
  const op = useOperation();
  return (
    <Modal title={person ? "Edit person" : "Add person"} onClose={onClose}>
      <Form
        onSubmit={() =>
          void op
            .run(person ? "/people/" + person.id + "/profile" : "/people", {
              name,
              ...(!person ? { external_id: external } : {}),
              email,
              department,
              manager_id: manager,
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
        {!person ? (
          <Field
            label="External ID"
            hint="A stable unique identity key. Display names do not establish account ownership."
          >
            <input
              required
              maxLength={200}
              value={external}
              onChange={(e) => setExternal(e.target.value)}
            />
          </Field>
        ) : null}
        <Field label="Email">
          <input
            type="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </Field>
        <Field label="Department">
          <input
            value={department}
            onChange={(e) => setDepartment(e.target.value)}
          />
        </Field>
        <Field label="Manager Person ID (optional)">
          <input value={manager} onChange={(e) => setManager(e.target.value)} />
        </Field>
        <small>
          Department and manager are profile information. They do not grant
          access.
        </small>
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
function LinkAccount({
  person,
  onClose,
}: {
  person: Identity;
  onClose: () => void;
}) {
  const [provider, setProvider] = useState("slack"),
    [one, setOne] = useState(""),
    [two, setTwo] = useState("");
  const op = useOperation();
  return (
    <Modal title="Link verified account" onClose={onClose}>
      <Form
        onSubmit={() =>
          void op
            .run(
              `/people/${person.id}/${provider}-account`,
              provider === "slack"
                ? { team_id: one, user_id: two }
                : { issuer: one, subject: two },
            )
            .then((r) => {
              if (r) onClose();
            })
        }
      >
        <p>
          Link an immutable provider identity verified by trusted
          administration. Matching an email or display name is insufficient.
        </p>
        <Field label="Provider">
          <select
            value={provider}
            onChange={(e) => {
              setProvider(e.target.value);
              setOne("");
              setTwo("");
            }}
          >
            <option value="slack">Slack</option>
            <option value="oidc">OpenID Connect</option>
          </select>
        </Field>
        <Field
          label={provider === "slack" ? "Slack team ID" : "Exact issuer URL"}
        >
          <input
            required
            value={one}
            onChange={(e) => setOne(e.target.value)}
          />
        </Field>
        <Field
          label={provider === "slack" ? "Slack user ID" : "Immutable subject"}
        >
          <input
            required
            value={two}
            onChange={(e) => setTwo(e.target.value)}
          />
        </Field>
        <ErrorView error={op.error} />
        <Button primary type="submit" disabled={op.pending}>
          Link account
        </Button>
      </Form>
    </Modal>
  );
}
function Connect({ onClose }: { onClose: () => void }) {
  return (
    <Modal title="Connect an agent" wide onClose={onClose}>
      <div className="form">
        <p>
          Agents register automatically through an authorized launcher. Connect
          each independent agent with a stable external key. Reusing that key
          creates a separate runtime instance.
        </p>
        <h3>1. Prepare a bounded launcher</h3>
        <p>
          Create a Software service in Settings, assign its intended roles in
          Permissions, then issue an enrollment token with a restricted
          external-key prefix.
        </p>
        <Button
          onClick={() => {
            onClose();
            navigate("/settings/services");
          }}
        >
          Set up a launcher
        </Button>
        <h3>2. Connect from your runtime</h3>
        <pre className="help">{`from notif import Client\n\nlauncher = Client(BASE_URL, ENROLLMENT_TOKEN)\nworker, session = launcher.connect(\n    "research/scout", "Scout", ["message", "status"]\n)`}</pre>
        <p className="help">
          Keep enrollment credentials in your runtime's secret store. Beep Grid
          connects your processes and does not host models or execute your agent
          code.
        </p>
      </div>
    </Modal>
  );
}
