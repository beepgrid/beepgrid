import { useState } from "react";
import {
  useCapabilities,
  useCollection,
  useOperation,
  useResource,
} from "../data";
import type { Connection, Destination, Target, Identity } from "../contracts";
import { TargetPicker } from "../TargetPicker";
import { navigate } from "../navigation";
import {
  Badge,
  Button,
  Copy,
  Details,
  ErrorView,
  Field,
  Form,
  Icon,
  IdentityIcon,
  Loading,
  Modal,
  PageHeader,
  QueryState,
  Tabs,
} from "../ui";

export type Provider = {
  kind: string;
  fields: { name: string; required: boolean; secret: boolean }[];
  inbound_messages: boolean;
  native_approvals: boolean;
  web_continuation: boolean;
  attachments: boolean;
  builtin: boolean;
  constraints?: string[];
};
type Diagnostic = {
  issues: string[];
  state: string;
  qualification: string;
  configuration_valid: boolean;
  last_test?: {
    state: string;
    detail: string;
    conversation_id: string;
    inbound_reply: boolean;
  };
};
type Setup = {
  steps: string[];
  callbacks: Record<string, string>;
  manifest?: Record<string, unknown>;
};
const states: Record<string, string> = {
  configured: "Configured",
  setup_incomplete: "Finish setup",
  disconnected: "Disconnected",
  verifying: "Awaiting verification",
  connected: "Connected",
  action_needed: "Needs attention",
};
const names: Record<string, string> = {
  native: "Beepgrid",
  webhook: "Webhooks",
  slack: "Slack",
  telegram: "Telegram",
  email: "Email (SMTP)",
};
const descriptions: Record<string, string> = {
  native: "Browser and authenticated worker communication.",
  webhook: "Signed inbound and outbound HTTP messages.",
  slack: "Messages, threads and verified interactive responses.",
  email: "Send messages by email. Continue the conversation in your workspace.",
  telegram: "Private bot messages, replies and in-chat approvals.",
};
const labels: Record<string, string> = {
  url: "Endpoint URL",
  inbound_sender_id: "Inbound service identity ID",
  team_id: "Slack team ID",
  bot_token: "Bot token",
  bot_id: "Bot ID",
  webhook_secret: "Webhook secret",
  signing_secret: "Signing secret",
  default_agent_id: "Default agent ID",
  smtp_addr: "SMTP host:port",
  from: "Sender email",
  username: "SMTP username",
  secret: "Signing secret or SMTP password",
};
export default function Integrations() {
  const [tab, setTab] = useState("catalog"),
    [provider, setProvider] = useState<Provider | null>(null),
    [selected, setSelected] = useState<Connection | null>(null);
  const catalog = useResource<Provider[]>("/integrations/catalog"),
    connections = useCollection<Connection>("connections"),
    cap = useCapabilities();
  return (
    <>
      <PageHeader title="Integrations" />
      <Tabs
        value={tab}
        onChange={setTab}
        options={[
          { id: "catalog", label: "Browse" },
          { id: "connections", label: "Connections" },
          { id: "destinations", label: "Delivery preferences" },
        ]}
      />
      {tab === "catalog" ? (
        <>
          <ErrorView error={catalog.error} />
          {catalog.isPending ? <Loading /> : null}
          <div className="provider-grid">
            {catalog.data
              ?.filter((p) => !p.builtin)
              .map((p) => (
                <article className="provider" key={p.kind}>
                  <header>
                    <span className="provider-mark" data-kind={p.kind}>
                      <Icon
                        name={
                          p.kind === "email"
                            ? "mail"
                            : p.kind === "slack"
                              ? "slack"
                              : p.kind === "telegram"
                                ? "send"
                                : "webhook"
                        }
                        size={21}
                      />
                    </span>
                    <h3>{names[p.kind] || p.kind}</h3>
                    {p.builtin ? <Badge>Built in</Badge> : null}
                  </header>
                  <p>{descriptions[p.kind]}</p>
                  <div className="actions">
                    <Badge>{p.inbound_messages ? "Two-way" : "Outbound"}</Badge>
                    {p.native_approvals ? (
                      <Badge>In-channel approvals</Badge>
                    ) : p.web_continuation ? (
                      <Badge>Reply in workspace</Badge>
                    ) : null}
                  </div>
                  {!p.builtin && cap.data?.operations.create_connection ? (
                    <Button icon="plus" onClick={() => setProvider(p)}>
                      Add connection
                    </Button>
                  ) : null}
                </article>
              ))}
          </div>
          <div className="provider upcoming-provider">
            <Icon name="plus" />
            <strong>More integrations coming soon...</strong>
          </div>
        </>
      ) : null}
      {tab === "connections" ? (
        <QueryState query={connections} empty="No connections yet">
          <table>
            <thead>
              <tr>
                <th>Connection</th>
                <th>Provider</th>
                <th>Setup</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {connections.items.map((c) => (
                <tr key={c.id}>
                  <td>{c.name}</td>
                  <td>{names[c.kind] || c.kind}</td>
                  <td>
                    <ConnectionState connection={c} />
                  </td>
                  <td>
                    <Button onClick={() => setSelected(c)}>Manage</Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </QueryState>
      ) : null}
      {tab === "destinations" ? <Destinations /> : null}
      {provider ? (
        <ConnectionForm
          provider={provider}
          onClose={() => setProvider(null)}
          onSaved={setSelected}
        />
      ) : null}
      {selected ? (
        <ConnectionDetail
          connection={selected}
          provider={catalog.data?.find((p) => p.kind === selected.kind)}
          onClose={() => setSelected(null)}
        />
      ) : null}
    </>
  );
}
export function ConnectionForm({
  provider: p,
  connection: c,
  onClose,
  onSaved,
}: {
  provider: Provider;
  connection?: Connection;
  onClose: () => void;
  onSaved?: (connection: Connection) => void;
}) {
  const [name, setName] = useState(c?.name || names[p.kind] || p.kind),
    [config, setConfig] = useState<Record<string, string>>(c?.config || {}),
    [secrets, setSecrets] = useState<Record<string, string>>({}),
    [enabled, setEnabled] = useState(c?.enabled ?? true),
    [rotate, setRotate] = useState(!c);
  const op = useOperation();
  const secret = rotate
    ? ["slack", "telegram"].includes(p.kind)
      ? JSON.stringify(
          Object.fromEntries(
            p.fields
              .filter((f) => f.secret)
              .map((f) => [f.name, secrets[f.name] || ""]),
          ),
        )
      : secrets.secret || ""
    : undefined;
  return (
    <Modal
      title={c ? "Edit connection" : "Add " + (names[p.kind] || p.kind)}
      onClose={onClose}
    >
      <Form
        onSubmit={() =>
          void op
            .run<Connection>("/connections", {
              ...(c ? { id: c.id, version: c.version } : {}),
              name,
              kind: p.kind,
              config,
              enabled,
              ...(rotate ? { secret } : {}),
            })
            .then((r) => {
              if (r) {
                setSecrets({});
                onClose();
                onSaved?.(r);
              }
            })
        }
      >
        <Field label="Connection name">
          <input
            required
            maxLength={100}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>
        {p.kind === "slack" ? (
          <p className="help">
            Create a Slack app, then copy its bot token and signing secret.{" "}
            <a
              href="https://api.slack.com/apps"
              target="_blank"
              rel="noreferrer"
            >
              Open Slack app settings ↗
            </a>
          </p>
        ) : p.kind === "telegram" ? (
          <p className="help">
            Use a dedicated bot from BotFather. Its numeric ID is the part
            before the colon in its token. Save to get the webhook setup
            instructions.
          </p>
        ) : p.kind === "email" ? (
          <p className="help">
            Use your email provider’s SMTP settings. Your provider supplies the
            server and credentials.
          </p>
        ) : null}
        {p.fields
          .filter(
            (f) =>
              !f.secret &&
              !["inbound_sender_id", "default_agent_id"].includes(f.name),
          )
          .map((f) => (
            <Field label={labels[f.name] || f.name} key={f.name}>
              <input
                required={f.required}
                value={config[f.name] || ""}
                onChange={(e) =>
                  setConfig((x) => ({ ...x, [f.name]: e.target.value }))
                }
              />
            </Field>
          ))}
        {p.fields
          .filter((f) =>
            ["inbound_sender_id", "default_agent_id"].includes(f.name),
          )
          .map((f) => (
            <div className="field" key={f.name}>
              <span>
                {f.name === "default_agent_id"
                  ? "Receive messages as"
                  : "Send incoming messages as"}
              </span>
              <TargetPicker
                value={
                  config[f.name]
                    ? [
                        {
                          kind:
                            f.name === "default_agent_id" ||
                            config[f.name].startsWith("agt_")
                              ? "agent"
                              : config[f.name].startsWith("per_")
                                ? "person"
                                : "service",
                          id: config[f.name],
                        },
                      ]
                    : []
                }
                onChange={(v) =>
                  setConfig((old) => ({ ...old, [f.name]: v[0]?.id || "" }))
                }
                allowGroups={false}
                allowInstances={false}
                agentsOnly={f.name === "default_agent_id"}
              />
            </div>
          ))}
        {c ? (
          <label className="inline-check">
            <input
              type="checkbox"
              checked={rotate}
              onChange={(e) => setRotate(e.target.checked)}
            />
            Replace stored credentials
          </label>
        ) : null}
        {rotate ? (
          p.fields
            .filter((f) => f.secret)
            .map((f) => (
              <Field
                label={labels[f.name] || f.name}
                key={f.name}
                hint={
                  c
                    ? "Enter a new value. Existing secrets are never returned."
                    : undefined
                }
              >
                <input
                  type="password"
                  required={
                    f.required &&
                    !(p.kind === "slack" && f.name === "bot_token")
                  }
                  placeholder={
                    p.kind === "slack" && f.name === "bot_token"
                      ? "Add after installing your Slack app"
                      : undefined
                  }
                  autoComplete="new-password"
                  value={secrets[f.name] || ""}
                  onChange={(e) =>
                    setSecrets((x) => ({ ...x, [f.name]: e.target.value }))
                  }
                />
              </Field>
            ))
        ) : (
          <p className="help">Stored credentials will be preserved.</p>
        )}
        <label className="inline-check">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(e) => setEnabled(e.target.checked)}
          />
          Enable this connection
        </label>
        <ErrorView error={op.error} />
        <div className="form-actions">
          <Button onClick={onClose}>Cancel</Button>
          <Button primary type="submit" disabled={op.pending}>
            Save connection
          </Button>
        </div>
      </Form>
    </Modal>
  );
}
function ConnectionState({ connection: c }: { connection: Connection }) {
  const cap = useCapabilities({ kind: "connection", id: c.id });
  const q = useResource<Diagnostic>(
    `/connections/${c.id}/diagnostics`,
    cap.data?.operations.manage === true,
  );
  const state = !c.enabled ? "disconnected" : q.data?.state;
  return (
    <Badge
      tone={
        state === "connected"
          ? "good"
          : state === "action_needed" || state === "setup_incomplete"
            ? "warn"
            : "neutral"
      }
    >
      {state ? states[state] || state : c.enabled ? "Enabled" : "Disconnected"}
    </Badge>
  );
}

export function ConnectionDetail({
  connection: c,
  provider,
  onClose,
}: {
  connection: Connection;
  provider?: Provider;
  onClose: () => void;
}) {
  const cap = useCapabilities({ kind: "connection", id: c.id });
  const canManage = cap.data?.operations.manage === true;
  const diagnostics = useResource<Diagnostic>(
    `/connections/${c.id}/diagnostics`,
    canManage,
  );
  const setup = useResource<Setup>(`/connections/${c.id}/setup`, canManage);
  const destinations = useCollection<Destination>("destinations", "", {
    connection_id: c.id,
  });
  const [edit, setEdit] = useState(false),
    [adding, setAdding] = useState(false),
    [destination, setDestination] = useState(""),
    [confirm, setConfirm] = useState(false),
    [queued, setQueued] = useState(false),
    op = useOperation();
  const diagnostic = diagnostics.data;
  if (cap.error || cap.isPending)
    return (
      <Modal title="Connection" onClose={onClose}>
        <ErrorView error={cap.error} />
        {cap.isPending ? <Loading label="Checking connection access" /> : null}
      </Modal>
    );
  return (
    <Modal title={c.name} wide onClose={onClose}>
      <div className="actions">
        <Badge>{names[c.kind] || c.kind}</Badge>
        <Badge tone={diagnostic?.state === "connected" ? "good" : "neutral"}>
          {diagnostic
            ? states[diagnostic.state] || diagnostic.state
            : c.enabled
              ? "Checking setup"
              : "Disconnected"}
        </Badge>
        {canManage ? (
          <Button onClick={() => setEdit(true)}>Edit connection</Button>
        ) : null}
      </div>
      <ErrorView error={diagnostics.error || setup.error || op.error} />
      {canManage ? (
        <>
          <section className="section">
            <h3>Complete setup</h3>
            {diagnostic?.issues.map((issue) => (
              <p key={issue}>{issue}</p>
            ))}
            {setup.isPending ? <Loading /> : null}
            {setup.data?.manifest ? (
              <div className="field">
                <strong>Slack app manifest</strong>
                <p className="help">
                  Import this into a dedicated Slack app, install it, then add
                  its bot token in Edit connection.
                </p>
                <Copy value={JSON.stringify(setup.data.manifest, null, 2)} />
              </div>
            ) : null}
            {Object.entries(setup.data?.callbacks || {}).map(
              ([name, value]) => (
                <div className="field" key={name}>
                  <label htmlFor={"callback-" + name}>
                    {name === "interactivity"
                      ? "Interactive actions URL"
                      : name === "events"
                        ? "Events URL"
                        : "Inbound webhook URL"}
                  </label>
                  <div className="actions">
                    <input id={"callback-" + name} readOnly value={value} />
                    <Copy value={value} />
                  </div>
                </div>
              ),
            )}
            {setup.data ? (
              <details>
                <summary>Provider setup instructions</summary>
                <ol>
                  {setup.data.steps.map((step) => (
                    <li key={step}>{step}</li>
                  ))}
                </ol>
              </details>
            ) : null}
            {provider?.constraints?.length ? (
              <details>
                <summary>Supported capabilities</summary>
                <ul>
                  {provider.constraints.map((line) => (
                    <li key={line}>{line}</li>
                  ))}
                </ul>
              </details>
            ) : null}
          </section>
          <section className="section">
            <div className="actions">
              <h3>Recipients</h3>
              <Button icon="plus" onClick={() => setAdding(true)}>
                Add recipient
              </Button>
            </div>
            <QueryState
              query={destinations}
              empty="Add a recipient to test this connection."
            >
              <Field label="Test recipient">
                <select
                  value={destination}
                  onChange={(e) => {
                    setDestination(e.target.value);
                    setConfirm(false);
                    setQueued(false);
                  }}
                >
                  <option value="">Choose a recipient</option>
                  {destinations.items.map((d) => (
                    <option key={d.id} value={d.id}>
                      {d.address}
                    </option>
                  ))}
                </select>
              </Field>
            </QueryState>
            {destinations.hasNextPage ? (
              <Button onClick={() => void destinations.fetchNextPage()}>
                Load more recipients
              </Button>
            ) : null}
            {confirm ? (
              <div className="approval">
                <p>
                  Send a test message to{" "}
                  {
                    destinations.items.find((d) => d.id === destination)
                      ?.address
                  }{" "}
                  through {c.name}?
                </p>
                <div className="actions">
                  <Button onClick={() => setConfirm(false)}>Cancel</Button>
                  <Button
                    primary
                    disabled={op.pending}
                    onClick={() =>
                      void op
                        .run(`/connections/${c.id}/test`, {
                          destination_id: destination,
                        })
                        .then((r) => {
                          if (r) {
                            setQueued(true);
                            setConfirm(false);
                          }
                        })
                    }
                  >
                    Send test message
                  </Button>
                </div>
              </div>
            ) : (
              <Button
                disabled={!destination || !c.enabled}
                onClick={() => setConfirm(true)}
              >
                Test connection
              </Button>
            )}
            {queued && !diagnostic?.last_test ? (
              <p role="status">Test queued. Waiting for provider acceptance.</p>
            ) : null}
            {diagnostic?.last_test ? (
              <div className="prose">
                <p role="status">
                  {diagnostic.qualification === "engine_round_trip_observed"
                    ? "A reply to the test was received and attributed in Beepgrid."
                    : diagnostic.qualification ===
                        "provider_acceptance_observed"
                      ? c.kind === "email"
                        ? "The mail server accepted the test. Check the recipient’s inbox and workspace link."
                        : "The provider accepted the test. Reply to that message to verify the return path."
                      : `Test ${diagnostic.last_test.state.replaceAll("_", " ")}.`}
                </p>
                {diagnostic.last_test.detail ? (
                  <p>{diagnostic.last_test.detail}</p>
                ) : null}
                <Button
                  onClick={() => {
                    onClose();
                    navigate(
                      "/conversations/" + diagnostic.last_test!.conversation_id,
                    );
                  }}
                >
                  Open test conversation
                </Button>
              </div>
            ) : null}
          </section>
        </>
      ) : null}
      <Details value={c.config} label="Connection details" />
      {edit && provider ? (
        <ConnectionForm
          provider={provider}
          connection={c}
          onClose={() => setEdit(false)}
          onSaved={onClose}
        />
      ) : null}
      {adding ? (
        <DestinationForm connection={c} onClose={() => setAdding(false)} />
      ) : null}
    </Modal>
  );
}
function Destinations() {
  const q = useCollection<Destination>("destinations"),
    [create, setCreate] = useState(false),
    [edit, setEdit] = useState<Destination | null>(null);
  const people = useCollection<Identity>("people"),
    agents = useCollection<Identity>("agents"),
    services = useCollection<Identity>("services"),
    connections = useCollection<Connection>("connections");
  const contacts = [...people.items, ...agents.items, ...services.items];
  return (
    <>
      <div className="toolbar">
        <Button onClick={() => setCreate(true)}>Add destination</Button>
      </div>
      <QueryState query={q} empty="No permitted destinations">
        <table>
          <thead>
            <tr>
              <th>Address</th>
              <th>Recipient</th>
              <th>Mode / priority</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {q.items.map((d) => (
              <DestinationRow
                key={d.id}
                destination={d}
                recipientName={
                  contacts.find((p) => p.id === d.principal_id)?.name ||
                  "Contact"
                }
                connectionName={
                  connections.items.find((c) => c.id === d.connection_id)
                    ?.name || "Connection"
                }
                onEdit={() => setEdit(d)}
              />
            ))}
          </tbody>
        </table>
      </QueryState>
      {create || edit ? (
        <DestinationForm
          destination={edit || undefined}
          recipientName={
            contacts.find((p) => p.id === edit?.principal_id)?.name
          }
          onClose={() => {
            setCreate(false);
            setEdit(null);
          }}
        />
      ) : null}
    </>
  );
}
function DestinationRow({
  destination: d,
  recipientName,
  connectionName,
  onEdit,
}: {
  destination: Destination;
  recipientName: string;
  connectionName: string;
  onEdit: () => void;
}) {
  const connection = useCapabilities({
      kind: "destination",
      id: d.id,
    }),
    op = useOperation(),
    [confirm, setConfirm] = useState(false);
  return (
    <tr>
      <td>
        {d.address}
        <p>
          <small>{connectionName}</small>
        </p>
      </td>
      <td>{recipientName}</td>
      <td>
        {d.mode} · {d.priority}
      </td>
      <td>
        {connection.data?.operations.manage ? (
          <div className="actions">
            <Button onClick={onEdit}>Edit</Button>
            <Button danger onClick={() => setConfirm(true)}>
              Remove
            </Button>
            {confirm ? (
              <div className="approval">
                <p>Remove this destination? History will be preserved.</p>
                <Button onClick={() => setConfirm(false)}>Keep</Button>
                <Button
                  danger
                  disabled={op.pending}
                  onClick={() =>
                    void op
                      .run("/destinations/" + d.id + "/remove", {
                        version: d.version,
                      })
                      .then((r) => {
                        if (r) setConfirm(false);
                      })
                  }
                >
                  Confirm removal
                </Button>
                <ErrorView error={op.error} />
              </div>
            ) : null}
          </div>
        ) : null}
      </td>
    </tr>
  );
}
export function DestinationForm({
  connection: initialConnection,
  destination: d,
  recipientName,
  onClose,
}: {
  destination?: Destination;
  connection?: Connection;
  recipientName?: string;
  onClose: () => void;
}) {
  const connections = useCollection<Connection>("connections"),
    [target, setTarget] = useState<Target[]>([]),
    [connection, setConnection] = useState(
      d?.connection_id || initialConnection?.id || "",
    ),
    [address, setAddress] = useState(d?.address || ""),
    [mode, setMode] = useState(d?.mode || "primary"),
    [priority, setPriority] = useState(d?.priority || 0),
    op = useOperation();
  const selectedConnection =
    connections.items.find((c) => c.id === connection) ||
    (initialConnection?.id === connection ? initialConnection : undefined);
  const personID = d?.principal_id || target[0]?.id || "";
  const person = useCollection<Identity>(
    "people",
    "",
    { id: personID },
    !!personID,
  );
  const linkCap = useCapabilities({ kind: "person", id: personID });
  const [linking, setLinking] = useState(false);
  const linkedAccount =
    !!selectedConnection &&
    !!person.items[0]?.metadata?.[selectedConnection.kind + "_user_id"] &&
    (selectedConnection.kind === "telegram"
      ? person.items[0]?.metadata?.telegram_bot_id ===
        selectedConnection.config.bot_id
      : person.items[0]?.metadata?.slack_team_id ===
        selectedConnection.config.team_id);
  const recipient = useCapabilities(
    d ? { kind: "destination", id: d.id } : target[0],
    undefined,
    connection,
  );
  return (
    <Modal
      title={d ? "Edit destination" : "Add destination"}
      wide
      onClose={onClose}
    >
      <Form
        onSubmit={() =>
          void op
            .run("/destinations", {
              ...(d ? { id: d.id, version: d.version } : {}),
              connection_id: connection,
              principal_id: d?.principal_id || target[0]?.id,
              address,
              mode,
              priority,
            })
            .then((r) => {
              if (r) onClose();
            })
        }
      >
        <Field label="Connection">
          <select
            required
            value={connection}
            onChange={(e) => {
              setConnection(e.target.value);
              if (!d) setTarget([]);
              setAddress("");
            }}
          >
            <option value="">Choose connection</option>
            {connections.items.map((c) => (
              <option value={c.id} key={c.id}>
                {c.name} · {c.kind}
              </option>
            ))}
          </select>
        </Field>
        <ErrorView error={connections.error} />
        {connections.hasNextPage ? (
          <Button onClick={() => void connections.fetchNextPage()}>
            Load more connections
          </Button>
        ) : null}
        {d ? (
          <p>
            Recipient: <strong>{recipientName || "Selected contact"}</strong>
          </p>
        ) : (
          <TargetPicker
            key={selectedConnection?.kind || "all"}
            value={target}
            onChange={setTarget}
            allowGroups={false}
            allowInstances={false}
            peopleOnly={
              !!selectedConnection &&
              ["slack", "telegram"].includes(selectedConnection.kind)
            }
          />
        )}
        {selectedConnection &&
        ["slack", "telegram"].includes(selectedConnection.kind) &&
        person.items[0] ? (
          <div className="section">
            <strong>{names[selectedConnection.kind]} account</strong>
            <p>
              {linkedAccount
                ? "Account linked"
                : "Link this person’s account before saving their destination."}
            </p>
            {linkCap.data?.operations.link_account ? (
              <Button onClick={() => setLinking(true)}>Link account</Button>
            ) : null}
          </div>
        ) : null}
        <Field
          label="Provider address"
          hint={
            selectedConnection?.kind === "telegram"
              ? "Use the linked person’s numeric Telegram user ID. Private bot chats only."
              : selectedConnection?.kind === "slack"
                ? "Use their Slack DM channel ID."
                : undefined
          }
        >
          <input
            required
            maxLength={512}
            value={address}
            onChange={(e) => setAddress(e.target.value)}
          />
        </Field>
        {selectedConnection?.kind === "telegram" && linkedAccount ? (
          <Button
            onClick={() =>
              setAddress(
                String(person.items[0]?.metadata?.telegram_user_id || ""),
              )
            }
          >
            Use linked account
          </Button>
        ) : null}

        <div className="form-grid">
          <Field label="Mode">
            <select value={mode} onChange={(e) => setMode(e.target.value)}>
              <option value="primary">Primary</option>
              <option value="fallback">Fallback after definite failure</option>
              <option value="copy">Copy on every delivery</option>
            </select>
          </Field>
          <Field label="Priority">
            <input
              type="number"
              step={1}
              value={priority}
              onChange={(e) => setPriority(Number(e.target.value))}
            />
          </Field>
        </div>
        <p className="help">
          A new primary replaces the previous primary according to engine
          routing policy. Unknown provider acceptance does not trigger an
          automatic duplicate fallback.
        </p>
        <ErrorView error={op.error} />
        <div className="form-actions">
          <Button onClick={onClose}>Cancel</Button>
          <Button
            primary
            disabled={
              op.pending ||
              !(d
                ? recipient.data?.operations.manage
                : recipient.data?.operations.manage_destination)
            }
            type="submit"
          >
            Save destination
          </Button>
        </div>
      </Form>
      {linking && selectedConnection && person.items[0] ? (
        <AccountLink
          person={person.items[0]}
          connection={selectedConnection}
          onClose={() => setLinking(false)}
        />
      ) : null}
    </Modal>
  );
}

function AccountLink({
  person,
  connection,
  onClose,
}: {
  person: Identity;
  connection: Connection;
  onClose: () => void;
}) {
  const kind = connection.kind;
  const [user, setUser] = useState(
    String(person.metadata?.[kind + "_user_id"] || ""),
  );
  const op = useOperation();
  return (
    <Modal title={`Link ${person.name} to ${names[kind]}`} onClose={onClose}>
      <Form
        onSubmit={() =>
          void op
            .run(`/people/${person.id}/${kind}-account`, {
              user_id: user,
              ...(kind === "telegram"
                ? { bot_id: connection.config.bot_id }
                : { team_id: connection.config.team_id }),
            })
            .then((r) => {
              if (r !== undefined) onClose();
            })
        }
      >
        <p className="help">
          Confirm this immutable account ID belongs to {person.name}. Linking
          does not grant message or approval permissions.
        </p>
        <Field
          label={kind === "telegram" ? "Telegram user ID" : "Slack member ID"}
        >
          <input
            required
            value={user}
            onChange={(e) => setUser(e.target.value)}
            autoComplete="off"
          />
        </Field>
        <ErrorView error={op.error} />
        <div className="form-actions">
          <Button onClick={onClose}>Cancel</Button>
          <Button primary type="submit" disabled={op.pending || !user.trim()}>
            Link account
          </Button>
        </div>
      </Form>
    </Modal>
  );
}
