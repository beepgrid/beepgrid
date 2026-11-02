import { lazy, Suspense, useState } from "react";
import {
  useCapabilities,
  useCollection,
  useOperation,
  useResource,
  useSession,
} from "../data";
import type {
  AssistanceSettings,
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
import { OverseerMark } from "../OverseerEntry";
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

const AccessTokens = lazy(() => import("./AccessTokens"));
const Activity = lazy(() => import("./Activity"));
const Permissions = lazy(() => import("./Permissions"));
const Groups = lazy(() => import("./Groups"));
const sections = [
  ["account", "My account"],
  ["workspace", "Workspace"],
  ["services", "Software services"],
  ["credentials", "Access tokens"],
  ["assistance", "Coordinator"],
  ["answers", "Published answers"],
];
export default function Settings() {
  const route = useRoute(),
    tab = route.id || "account",
    me = useSession(),
    meta = useResource<Workspace>("/workspace");
  return (
    <>
      <PageHeader title="Settings" />
      <div className="settings-layout">
        <nav className="settings-nav" aria-label="Settings sections">
          {sections
            .filter(
              ([key]) =>
                me.admin ||
                ["account", "workspace", "credentials"].includes(key) ||
                (key === "answers" && !!me.can_publish_answers),
            )
            .map(([key, label]) => (
              <button
                key={key}
                aria-current={tab === key}
                onClick={() => navigate("/settings/" + key)}
              >
                {label}
              </button>
            ))}
          <button
            aria-current={tab === "contacts"}
            onClick={() => navigate("/settings/contacts")}
          >
            Contact permissions
          </button>
          {me.admin ? (
            <button
              aria-current={tab === "groups"}
              onClick={() => navigate("/settings/groups")}
            >
              Contact groups
            </button>
          ) : null}
          {me.admin ? (
            <button
              aria-current={tab === "activity"}
              onClick={() => navigate("/settings/activity")}
            >
              Activity log
            </button>
          ) : null}
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
                    <p>{String(me.person.metadata?.email || "Signed in")}</p>
                  </div>
                </div>
                <dl className="detail-list">
                  <dt>Company</dt>
                  <dd>{me.company}</dd>
                  <dt>Environment</dt>
                  <dd>{me.environment}</dd>
                  <dt>Session</dt>
                  <dd>Authenticated browser session</dd>
                  <dt>Role</dt>
                  <dd>{me.admin ? "Administrator" : "Member"}</dd>
                </dl>
                <Details value={me.person.metadata} label="Profile details" />
                <Button onClick={() => navigate("/permissions")}>
                  My permissions
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
                </>
              ) : (
                <Loading />
              )}
            </section>
          ) : null}
          {tab === "services" ? <Services /> : null}
          {tab === "credentials" ? <Suspense fallback={<Loading />}><AccessTokens /></Suspense> : null}
          {tab === "assistance" ? <Assistance /> : null}
          {tab === "answers" ? <Answers /> : null}
          {tab === "contacts" ? (
            <Suspense fallback={<Loading />}>
              <Permissions />
            </Suspense>
          ) : null}
          {tab === "activity" && me.admin ? (
            <Suspense fallback={<Loading />}>
              <Activity />
            </Suspense>
          ) : null}
          {tab === "groups" && me.admin ? (
            <Suspense fallback={<Loading />}>
              <Groups purpose="contacts" />
            </Suspense>
          ) : null}
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

function Assistance() {
  const q = useResource<AssistanceSettings>("/assistance/settings"),
    cap = useCapabilities(),
    op = useOperation(),
    [confirm, setConfirm] = useState(false);
  if (q.isPending) return <Loading />;
  if (q.error) return <ErrorView error={q.error} />;
  const s = q.data!;
  return (
    <>
      <div className="coordinator-settings">
        <div className="coordinator-settings-head">
          <OverseerMark />
          <div>
            <h2>Coordinator</h2>
            <p>One private conversation for coordinating your Agents.</p>
          </div>
        </div>
        <ErrorView error={op.error} />
        <div className="coordinator-setting-row">
          <div>
            <strong>Availability</strong>
            <p>
              {s.overseer
                ? "Available to people in this workspace."
                : "Turn on Coordinator for this workspace."}
            </p>
          </div>
          <Badge>{s.overseer ? "On" : "Off"}</Badge>
          {cap.data?.operations.configure_assistance ? (
            <Button
              disabled={op.pending || (!s.overseer && !s.overseer_configured)}
              onClick={() => setConfirm(true)}
            >
              {s.overseer ? "Disable" : "Enable"}
            </Button>
          ) : null}
        </div>
        <div className="coordinator-setting-row">
          <div>
            <strong>Model provider</strong>
            <p>
              {s.overseer_configured
                ? "Configured for this workspace."
                : "A workspace operator needs to connect a model provider first."}
            </p>
          </div>
          <Badge>
            {s.overseer_configured ? "Configured" : "Not configured"}
          </Badge>
        </div>
      </div>
      {confirm ? (
        <Modal
          title={`${s.overseer ? "Disable" : "Enable"} Coordinator?`}
          onClose={() => setConfirm(false)}
        >
          <div className="form">
            <p>
              {s.overseer
                ? "Future model work stops. Durable history remains."
                : "Authorized context is shared with the configured provider. Coordinator can send permitted instructions automatically. People can choose to review instructions before sending."}
            </p>
            <ErrorView error={op.error} />
            <div className="form-actions">
              <Button onClick={() => setConfirm(false)}>Cancel</Button>
              <Button
                primary
                disabled={op.pending}
                onClick={() =>
                  void op
                    .run("/assistance/settings", {
                      version: s.version,
                      overseer: !s.overseer,
                    })
                    .then((v) => {
                      if (v) setConfirm(false);
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
