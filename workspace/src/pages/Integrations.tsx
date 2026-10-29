import { useState } from "react";
import {
  useCapabilities,
  useCollection,
  useOperation,
  useResource,
} from "../data";
import type { Connection, Destination, Target, Identity } from "../contracts";
import { TargetPicker } from "../TargetPicker";
import {
  Badge,
  Button,
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

type Provider = {
  kind: string;
  fields: { name: string; required: boolean; secret: boolean }[];
  inbound_messages: boolean;
  native_approvals: boolean;
  web_continuation: boolean;
  attachments: boolean;
  builtin: boolean;
};
const names: Record<string, string> = {
  native: "Beep Grid",
  webhook: "Webhooks",
  slack: "Slack",
  email: "Email (SMTP)",
};
const descriptions: Record<string, string> = {
  native: "Browser and authenticated worker communication.",
  webhook: "Signed inbound and outbound HTTP messages.",
  slack: "Messages, threads and verified interactive responses.",
  email: "Send messages by email. Continue the conversation in your workspace.",
};
const labels: Record<string, string> = {
  url: "Endpoint URL",
  inbound_sender_id: "Inbound service identity ID",
  team_id: "Slack team ID",
  bot_token: "Bot token",
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
          { id: "connections", label: "Connected" },
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
                    <span className="provider-mark" data-kind={p.kind}><Icon name={p.kind === "email" ? "mail" : p.kind === "slack" ? "slack" : "webhook"} size={21}/></span>
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
                <th>Access</th>
                <th>Secret</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {connections.items.map((c) => (
                <tr key={c.id}>
                  <td>{c.name}</td>
                  <td>{names[c.kind] || c.kind}</td>
                  <td>
                    <Badge>{c.enabled ? "Enabled" : "Disabled"}</Badge>
                  </td>
                  <td>
                    {c.secret_configured ? "Configured" : "Not configured"}
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
        <ConnectionForm provider={provider} onClose={() => setProvider(null)} />
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
  onSaved?: () => void;
}) {
  const [name, setName] = useState(c?.name || names[p.kind] || p.kind),
    [config, setConfig] = useState<Record<string, string>>(c?.config || {}),
    [secrets, setSecrets] = useState<Record<string, string>>({}),
    [enabled, setEnabled] = useState(c?.enabled ?? true),
    [rotate, setRotate] = useState(!c);
  const op = useOperation();
  const secret = rotate
    ? p.kind === "slack"
      ? JSON.stringify({
          bot_token: secrets.bot_token || "",
          signing_secret: secrets.signing_secret || "",
        })
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
            .run("/connections", {
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
                onSaved?.();
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
                  required={f.required}
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
function ConnectionDetail({
  connection: c,
  provider,
  onClose,
}: {
  connection: Connection;
  provider?: Provider;
  onClose: () => void;
}) {
  const cap = useCapabilities({ kind: "connection", id: c.id }),
    diagnostics = useResource<{
      issues: string[];
      configuration_valid: boolean;
      provider_acceptance: string;
    }>(
      "/connections/" + c.id + "/diagnostics",
      cap.data?.operations.manage === true,
    ),
    destinations = useCollection<Destination>("destinations", "", {
      connection_id: c.id,
    }),
    [edit, setEdit] = useState(false),
    [destination, setDestination] = useState(""),
    [confirm, setConfirm] = useState(false),
    [result, setResult] = useState<unknown>(null),
    op = useOperation();
  if (cap.error || cap.isPending)
    return (
      <Modal title="Connection unavailable" onClose={onClose}>
        <ErrorView error={cap.error} />
        {cap.isPending ? <Loading label="Checking connection access" /> : null}
      </Modal>
    );
  return (
    <Modal title={c.name} wide onClose={onClose}>
      <div className="intro">
        <IdentityIcon id={c.id} kind="service" label={c.name} />
        <div>
          <h2>{c.name}</h2>
          <p>
            {names[c.kind]} · {c.id} · Revision {c.version}
          </p>
        </div>
        <Badge>{c.enabled ? "Enabled" : "Disabled"}</Badge>
      </div>
      <ErrorView error={cap.error || diagnostics.error || op.error} />
      {cap.data?.operations.manage ? (
        <>
          <Button onClick={() => setEdit(true)}>Edit connection</Button>
          <section className="section">
            <h3>Configuration readiness</h3>
            {diagnostics.isPending ? (
              <Loading />
            ) : diagnostics.data ? (
              <>
                <Badge
                  tone={diagnostics.data.configuration_valid ? "good" : "warn"}
                >
                  {diagnostics.data.configuration_valid
                    ? "Configuration valid"
                    : "Needs attention"}
                </Badge>
                {diagnostics.data.issues.map((issue) => (
                  <p key={issue}>{issue}</p>
                ))}
                <p>
                  <small>
                    Provider acceptance: {diagnostics.data.provider_acceptance}.
                    These checks send nothing.
                  </small>
                </p>
              </>
            ) : null}
          </section>
          <section className="section">
            <h3>Send a connection test</h3>
            <p>
              Explicitly sends a real test message to the selected configured
              destination. Current management and message permissions apply.
            </p>
            <Field label="Destination">
              <select
                value={destination}
                onChange={(e) => {
                  setDestination(e.target.value);
                  setConfirm(false);
                }}
              >
                <option value="">Choose destination</option>
                {destinations.items.map((d) => (
                  <option value={d.id} key={d.id}>
                    {d.address} · {d.principal_id}
                  </option>
                ))}
              </select>
            </Field>
            <ErrorView error={destinations.error} />
            {destinations.hasNextPage ? (
              <Button onClick={() => void destinations.fetchNextPage()}>
                Load more destinations
              </Button>
            ) : null}
            {confirm ? (
              <div className="approval">
                <p>
                  Send a real message through {c.name} to{" "}
                  {
                    destinations.items.find((d) => d.id === destination)
                      ?.address
                  }
                  ?
                </p>
                <div className="actions">
                  <Button onClick={() => setConfirm(false)}>Cancel</Button>
                  <Button
                    primary
                    disabled={op.pending}
                    onClick={() =>
                      void op
                        .run("/connections/" + c.id + "/test", {
                          destination_id: destination,
                        })
                        .then((r) => {
                          if (r) {
                            setResult(r);
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
                Review test
              </Button>
            )}
            {result ? (
              <Details
                value={result}
                label="Accepted test delivery (not proof of receipt)"
              />
            ) : null}
          </section>
        </>
      ) : null}
      <Details value={c.config} label="Permitted connection configuration" />
      {edit && provider ? (
        <ConnectionForm
          provider={provider}
          connection={c}
          onClose={() => setEdit(false)}
          onSaved={onClose}
        />
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
function DestinationForm({
  destination: d,
  recipientName,
  onClose,
}: {
  destination?: Destination;
  recipientName?: string;
  onClose: () => void;
}) {
  const connections = useCollection<Connection>("connections"),
    [target, setTarget] = useState<Target[]>([]),
    [connection, setConnection] = useState(d?.connection_id || ""),
    [address, setAddress] = useState(d?.address || ""),
    [mode, setMode] = useState(d?.mode || "primary"),
    [priority, setPriority] = useState(d?.priority || 0),
    op = useOperation();
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
            onChange={(e) => setConnection(e.target.value)}
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
            value={target}
            onChange={setTarget}
            allowGroups={false}
            allowInstances={false}
          />
        )}
        <Field
          label="Provider address"
          hint="Shared external channels may expose messages to their external members. Beep Grid permission checks still govern responses."
        >
          <input
            required
            maxLength={512}
            value={address}
            onChange={(e) => setAddress(e.target.value)}
          />
        </Field>
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
    </Modal>
  );
}
