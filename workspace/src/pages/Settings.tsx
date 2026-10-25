import { useState } from "react";
import {
  useCapabilities,
  useCollection,
  useOperation,
  useResource,
  useSession,
} from "../data";
import type {
  AssistanceSettings,
  Credential,
  Identity,
  PublishedAnswer,
  Target,
  Workspace,
} from "../contracts";
import { readable } from "../client";
import { useRoute, navigate } from "../navigation";
import { TargetPicker } from "../TargetPicker";
import { Compose } from "../Compose";
import { IdentityDetail } from "./Directory";
import { AccountView, type Account } from "../AccountView";
import {
  Badge,
  Button,
  Copy,
  DateValue,
  Details,
  ErrorView,
  Field,
  Form,
  IdentityIcon,
  Loading,
  Modal,
  PageHeader,
  QueryState,
} from "../ui";

const sections = [
  ["account", "My account"],
  ["workspace", "Workspace"],
  ["services", "Software services"],
  ["credentials", "Access tokens"],
  ["assistance", "Assistance"],
  ["answers", "Published answers"],
];
export default function Settings() {
  const route = useRoute(),
    tab = route.id || "account",
    me = useSession(),
    meta = useResource<Workspace>("/workspace");
  return (
    <>
      <PageHeader
        title="Settings"
        description="Your account and authorized workspace configuration."
      />
      <div className="settings-layout">
        <nav className="settings-nav" aria-label="Settings sections">
          {sections.map(([key, label]) => (
            <button
              key={key}
              aria-current={tab === key}
              onClick={() => navigate("/settings/" + key)}
            >
              {label}
            </button>
          ))}
          {meta.data?.workspace.can_read_account ? (
            <button
              aria-current={tab === "managed"}
              onClick={() => navigate("/settings/managed")}
            >
              Account and usage
            </button>
          ) : null}
        </nav>
        <div className="settings-content">
          {tab === "account" ? (
            <>
              <section className="section">
                <h2>My account</h2>
                <div className="intro">
                  <IdentityIcon
                    id={me.person.id}
                    kind="person"
                    label={me.person.name}
                  />
                  <div>
                    <h3>{me.person.name}</h3>
                    <p>{me.person.external_id}</p>
                  </div>
                </div>
                <dl className="detail-list">
                  <dt>Person ID</dt>
                  <dd>
                    <code>{me.person.id}</code>
                  </dd>
                  <dt>Company</dt>
                  <dd>{me.company}</dd>
                  <dt>Environment</dt>
                  <dd>{me.environment}</dd>
                  <dt>Session</dt>
                  <dd>Authenticated browser session</dd>
                  <dt>Authority</dt>
                  <dd>
                    {me.admin
                      ? "Administrator credential"
                      : "Current role grants"}
                  </dd>
                </dl>
                <Details
                  value={me.person.metadata}
                  label="Permitted profile and linked account information"
                />
                <Button onClick={() => navigate("/permissions")}>
                  View my effective access
                </Button>
              </section>
            </>
          ) : null}
          {tab === "workspace" ? (
            <section className="section">
              <h2>Workspace</h2>
              <ErrorView error={meta.error} />
              {meta.data ? (
                <>
                  <dl className="detail-list">
                    <dt>Company</dt>
                    <dd>{meta.data.workspace.company}</dd>
                    <dt>Environment</dt>
                    <dd>{meta.data.workspace.environment}</dd>
                    <dt>Deployment</dt>
                    <dd>{meta.data.deployment.replaceAll("_", " ")}</dd>
                    <dt>Schema version</dt>
                    <dd>{meta.data.schema_version}</dd>
                  </dl>
                  <p className="help">
                    Deployment and model credentials are configured by your
                    deployment administrator. Workspace creation, company SSO
                    onboarding, directory synchronization and payment management
                    are deferred.
                  </p>
                </>
              ) : (
                <Loading />
              )}
            </section>
          ) : null}
          {tab === "services" ? <Services /> : null}
          {tab === "credentials" ? <Credentials /> : null}
          {tab === "assistance" ? <Assistance /> : null}
          {tab === "answers" ? <Answers /> : null}
          {tab === "managed" ? <AccountStatus /> : null}
        </div>
      </div>
    </>
  );
}
function Services() {
  const [compose, setCompose] = useState<Target[] | null>(null),
    [composeMode, setComposeMode] = useState("message");
  const q = useCollection<Identity>("services"),
    cap = useCapabilities(),
    [create, setCreate] = useState(false),
    [selected, setSelected] = useState<Identity | null>(null),
    [name, setName] = useState(""),
    [external, setExternal] = useState(""),
    op = useOperation();
  return (
    <>
      <div className="page-header">
        <div>
          <h2>Software services</h2>
          <p>Trusted launchers and application integrations.</p>
        </div>
        {cap.data?.operations.create_service ? (
          <Button primary icon="plus" onClick={() => setCreate(true)}>
            Add service
          </Button>
        ) : null}
      </div>
      <QueryState query={q} empty="No permitted services">
        {q.items.map((s) => (
          <div className="section" key={s.id}>
            <div className="identity">
              <IdentityIcon id={s.id} kind="service" label={s.name} />
              <div style={{ flex: 1 }}>
                <strong>{s.name}</strong>
                <p>{s.external_id}</p>
              </div>
              <Badge>{s.disabled ? "Disabled" : "Enabled"}</Badge>
              <Button onClick={() => setSelected(s)}>Open</Button>
            </div>
          </div>
        ))}
      </QueryState>
      {create ? (
        <Modal title="Add software service" onClose={() => setCreate(false)}>
          <Form
            onSubmit={() =>
              void op
                .run("/services", { name, external_id: external })
                .then((r) => {
                  if (r) {
                    setCreate(false);
                    setName("");
                    setExternal("");
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
            <Field label="Stable external key">
              <input
                required
                maxLength={200}
                value={external}
                onChange={(e) => setExternal(e.target.value)}
              />
            </Field>
            <p className="help">
              Create the service identity first. Assign roles and issue bounded
              credentials separately.
            </p>
            <ErrorView error={op.error} />
            <Button primary type="submit" disabled={op.pending}>
              Create service
            </Button>
          </Form>
        </Modal>
      ) : null}
      {selected ? (
        <IdentityDetail
          identity={selected}
          onClose={() => setSelected(null)}
          onCompose={(target, mode = "message") => {
            setSelected(null);
            setComposeMode(mode);
            setCompose([target]);
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
    </>
  );
}
function Credentials() {
  const q = useCollection<Credential>("credentials"),
    [create, setCreate] = useState(false),
    [target, setTarget] = useState<Target[]>([]),
    [kind, setKind] = useState("api"),
    [prefix, setPrefix] = useState(""),
    [secret, setSecret] = useState(""),
    [metadata, setMetadata] = useState<unknown>(null),
    cap = useCapabilities(target[0]),
    op = useOperation();
  return (
    <>
      <div className="page-header">
        <div>
          <h2>Access tokens</h2>
          <p>Scoped credentials, separate from people and role grants.</p>
        </div>
        <Button icon="plus" onClick={() => setCreate(true)}>
          Issue token
        </Button>
      </div>
      <p className="help">
        To rotate a token, issue a replacement, update its runtime securely,
        verify the replacement, then revoke the old credential. This is not an
        atomic rotation operation.
      </p>
      <QueryState query={q} empty="No permitted credential metadata">
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Credential</th>
                <th>Owner</th>
                <th>Expiry</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {q.items.map((c) => (
                <CredentialRow credential={c} key={c.id} />
              ))}
            </tbody>
          </table>
        </div>
      </QueryState>
      {create ? (
        <Modal title="Issue access token" wide onClose={() => setCreate(false)}>
          <Form
            onSubmit={() =>
              void op
                .run<{ token: string; id: string; kind: string }>(
                  "/credentials",
                  {
                    principal_id: target[0]?.id,
                    kind,
                    prefix: kind === "enrollment" ? prefix : "",
                  },
                )
                .then((r) => {
                  if (r) {
                    setSecret(r.token);
                    setMetadata({ id: r.id, kind: r.kind });
                    setCreate(false);
                  }
                })
            }
          >
            <TargetPicker
              value={target}
              onChange={setTarget}
              allowGroups={false}
              allowInstances={false}
            />
            <Field label="Credential kind">
              <select value={kind} onChange={(e) => setKind(e.target.value)}>
                <option value="api">API access (30 days)</option>
                <option value="enrollment">Bounded enrollment (30 days)</option>
              </select>
            </Field>
            {kind === "enrollment" ? (
              <Field
                label="Allowed external-key prefix"
                hint="Required. Enrollment is limited to this explicit namespace."
              >
                <input
                  required
                  value={prefix}
                  onChange={(e) => setPrefix(e.target.value)}
                  placeholder="research/"
                />
              </Field>
            ) : null}
            <p className="help">
              No administrator credential is created here. Enrollment creates
              24-hour worker credentials for individual instances.
            </p>
            <ErrorView error={op.error || cap.error} />
            <Button
              primary
              disabled={
                op.pending ||
                !target.length ||
                !cap.data?.operations.issue_credential
              }
              type="submit"
            >
              Issue token
            </Button>
          </Form>
        </Modal>
      ) : null}
      {secret ? (
        <Modal
          title="Save your new token"
          onClose={() => {
            setSecret("");
            setMetadata(null);
          }}
        >
          <div className="secret">
            <p>
              Copy this secret now. It will not be shown again. Keep it in a
              secure runtime secret store.
            </p>
            <code>{secret}</code>
            <Copy value={secret} />
            <Details value={metadata} label="Issued credential metadata" />
            <Button
              primary
              onClick={() => {
                setSecret("");
                setMetadata(null);
              }}
            >
              I have saved it securely
            </Button>
          </div>
        </Modal>
      ) : null}
    </>
  );
}
function CredentialRow({ credential: c }: { credential: Credential }) {
  const [confirm, setConfirm] = useState(false),
    op = useOperation();
  return (
    <tr>
      <td>
        <code>{c.id}</code>
        <p>
          <small>
            {c.kind}
            {c.prefix ? " · " + c.prefix : ""}
          </small>
        </p>
      </td>
      <td>
        <code>{c.principal_id}</code>
      </td>
      <td>
        {c.revoked ? (
          <Badge>Revoked</Badge>
        ) : (
          <DateValue value={c.expires_at} />
        )}
      </td>
      <td>
        {!c.revoked && c.kind !== "admin" ? (
          <>
            <Button danger onClick={() => setConfirm(true)}>
              Revoke
            </Button>
            {confirm ? (
              <Modal
                title="Revoke this credential?"
                onClose={() => setConfirm(false)}
              >
                <div className="form">
                  <p>
                    This stops future authenticated calls and streams for this
                    exact credential. It does not delete history or revoke every
                    credential for its owner.
                  </p>
                  <code>{c.id}</code>
                  <ErrorView error={op.error} />
                  <div className="form-actions">
                    <Button onClick={() => setConfirm(false)}>
                      Keep credential
                    </Button>
                    <Button
                      danger
                      primary
                      disabled={op.pending}
                      onClick={() =>
                        void op
                          .run("/credentials/" + c.id + "/revoke", {})
                          .then((r) => {
                            if (r) setConfirm(false);
                          })
                      }
                    >
                      Revoke credential
                    </Button>
                  </div>
                </div>
              </Modal>
            ) : null}
          </>
        ) : null}
      </td>
    </tr>
  );
}
function Assistance() {
  const q = useResource<AssistanceSettings>("/assistance/settings"),
    cap = useCapabilities(),
    op = useOperation(),
    [pending, setPending] = useState<"automatic_answers" | "overseer" | null>(
      null,
    );
  if (q.isPending) return <Loading />;
  if (q.error) return <ErrorView error={q.error} />;
  const s = q.data!;
  return (
    <>
      <h2>Optional assistance</h2>
      <p className="help">
        Both features are independently opt-in. The configured model may receive
        permitted question context or authorized fleet reports. No provider or
        model is selected by default. Model credentials stay in deployment
        configuration.
      </p>
      <ErrorView error={op.error} />
      {(["automatic_answers", "overseer"] as const).map((key) => (
        <section className="section" key={key}>
          <div className="page-header">
            <div>
              <h3>
                {key === "overseer"
                  ? "Fleet overseer"
                  : "Automatic informational answers"}
              </h3>
              <p>
                {key === "overseer"
                  ? "Permission-filtered fleet interpretation with explicit confirmation before sending."
                  : "Reuse eligible informational answers. This never automatically approves actions."}
              </p>
            </div>
            <Badge>{s[key] ? "On" : "Off"}</Badge>
          </div>
          <small>
            {key === "overseer"
              ? s.overseer_configured
                ? "Provider configured"
                : "Provider not configured"
              : s.matcher_configured
                ? "Matcher configured"
                : "Matcher not configured"}
          </small>
          {cap.data?.operations.configure_assistance ? (
            <div className="actions">
              <Button
                disabled={
                  op.pending ||
                  (!s[key] &&
                    !(key === "overseer"
                      ? s.overseer_configured
                      : s.matcher_configured))
                }
                onClick={() => setPending(key)}
              >
                {s[key] ? "Disable" : "Enable"}
              </Button>
            </div>
          ) : null}
        </section>
      ))}
      <small>
        Settings revision {s.version}. Disabling invalidates pending model work;
        re-enabling does not resend or revive stale plans. Existing exact
        published answers have their own validity and invalidation controls.
      </small>
      {pending ? (
        <Modal
          title={`${s[pending] ? "Disable" : "Enable"} ${pending.replaceAll("_", " ")}?`}
          onClose={() => setPending(null)}
        >
          <div className="form">
            <p>
              {s[pending]
                ? "Future model work stops and pending work is reevaluated. Durable history remains."
                : "This shares the authorized context needed for this feature with the configured model provider. Current permissions and explicit send confirmation still apply."}
            </p>
            <ErrorView error={op.error} />
            <div className="form-actions">
              <Button onClick={() => setPending(null)}>Cancel</Button>
              <Button
                primary
                disabled={op.pending}
                onClick={() =>
                  void op
                    .run("/assistance/settings", {
                      version: s.version,
                      [pending]: !s[pending],
                    })
                    .then((v) => {
                      if (v) setPending(null);
                    })
                }
              >
                Confirm
              </Button>
            </div>
          </div>
        </Modal>
      ) : null}
    </>
  );
}
function Answers() {
  const q = useCollection<PublishedAnswer>("answers"),
    op = useOperation(),
    [confirm, setConfirm] = useState<PublishedAnswer | null>(null);
  return (
    <>
      <h2>Published answers</h2>
      <p className="help">
        Informational reuse is limited by exact context, explicit audience and
        validity. Invalidation stops future reuse and marks affected historical
        responses stale; it does not undo external work.
      </p>
      <QueryState query={q} empty="No permitted published answers">
        {q.items.map((a) => (
          <section className="section" key={a.id}>
            <div className="actions">
              <h3>{a.question}</h3>
              <Badge>
                {a.invalidated
                  ? "Invalidated"
                  : Date.parse(a.valid_until) <= Date.now()
                    ? "Expired"
                    : "Active"}
              </Badge>
            </div>
            <pre>{readable(a.answer)}</pre>
            <dl className="detail-list">
              <dt>Scope key</dt>
              <dd>{a.key}</dd>
              <dt>Valid until</dt>
              <dd>
                <DateValue value={a.valid_until} />
              </dd>
              <dt>Author / publisher</dt>
              <dd>
                {a.author_id}
                <br />
                {a.publisher_id}
              </dd>
              <dt>Audience</dt>
              <dd>{a.audience.join(", ")}</dd>
            </dl>
            <div className="actions">
              <Button onClick={() => navigate("/requests/" + a.source_request)}>
                Source request
              </Button>
              <AnswerInvalidate answer={a} onClick={() => setConfirm(a)} />
            </div>
          </section>
        ))}
      </QueryState>
      {confirm ? (
        <Modal
          title="Invalidate published answer?"
          onClose={() => setConfirm(null)}
        >
          <div className="form">
            <p>
              Future reuse stops. Previously recorded responses keep their
              provenance and are marked stale where applicable.
            </p>
            <ErrorView error={op.error} />
            <Button
              danger
              primary
              disabled={op.pending}
              onClick={() =>
                void op
                  .run("/answers/" + confirm.id + "/invalidate", {})
                  .then((r) => {
                    if (r) setConfirm(null);
                  })
              }
            >
              Confirm invalidation
            </Button>
          </div>
        </Modal>
      ) : null}
    </>
  );
}
export function AccountStatus() {
  const q = useResource<Account>("/workspace/account");
  return (
    <section className="section">
      <h2>Account and usage</h2>
      <ErrorView error={q.error} />
      {q.isPending ? <Loading /> : null}
      {q.data && !q.error ? (
        <>
          <AccountView account={q.data} />
        </>
      ) : null}
    </section>
  );
}

function AnswerInvalidate({
  answer,
  onClick,
}: {
  answer: PublishedAnswer;
  onClick: () => void;
}) {
  const cap = useCapabilities({ kind: "answer", id: answer.id });
  return cap.data?.operations.invalidate ? (
    <Button danger onClick={onClick}>
      Invalidate
    </Button>
  ) : null;
}
