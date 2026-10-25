import { useDeferredValue, useEffect, useState } from "react";
import {
  useCapabilities,
  useCollection,
  useDraft,
  useOperation,
  useResource,
  useSession,
} from "../data";
import type { Capability, Conversation, Request } from "../contracts";
import { bytes, readable, relative } from "../client";
import { navigate, useRoute } from "../navigation";
import { IdentityReference } from "../IdentityReference";
import { groupQuestions } from "../request-groups";
import { TargetPicker } from "../TargetPicker";
import type { Target } from "../contracts";
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
  Loading,
  Modal,
  PageHeader,
  QueryState,
  Search,
  Status,
  Tabs,
} from "../ui";

export default function Inbox() {
  const route = useRoute(),
    me = useSession();
  const [tab, setTab] = useState("open"),
    [kind, setKind] = useState(""),
    [search, setSearch] = useState(""),
    [compose, setCompose] = useState(false);
  const filters: Record<string, string> = {};
  if (tab === "open") filters.state = "open";
  if (tab === "created") filters.requester_id = me.person.id;
  if (kind) filters.kind = kind;
  const q = useCollection<Request>(
    "requests",
    useDeferredValue(search),
    filters,
  );
  if (route.id) return <RequestDetail id={route.id} key={route.id} />;
  const visible = q.items.filter(
    (r) => tab !== "history" || r.state !== "open",
  );
  return (
    <>
      <PageHeader
        title="Inbox"
        description="Questions, approvals and tasks that you can access."
        actions={
          <Button primary icon="plus" onClick={() => setCompose(true)}>
            New request
          </Button>
        }
      />
      <Tabs
        value={tab}
        onChange={setTab}
        options={[
          { id: "open", label: "Open" },
          { id: "history", label: "History" },
          { id: "created", label: "Created by you" },
        ]}
      />
      <div className="toolbar">
        <div className="filters">
          <select
            aria-label="Request kind"
            value={kind}
            onChange={(e) => setKind(e.target.value)}
          >
            <option value="">All request types</option>
            <option value="question">Questions</option>
            <option value="approval">Approvals</option>
            <option value="task">Tasks</option>
          </select>
        </div>
        <Search
          value={search}
          onChange={setSearch}
          placeholder="Search permitted requests"
        />
      </div>
      <QueryState
        query={{ ...q, items: visible }}
        empty={search ? "No matching requests" : "No requests in this view"}
      >
        <div className="requests-list">
          {groupQuestions(visible).map((group) => (
            <section
              className={group.length > 1 ? "question-group" : ""}
              key={group[0].id}
            >
              {group.length > 1 ? (
                <p className="help">
                  {group.length} matching informational questions · Each remains
                  an independent request.
                </p>
              ) : null}
              {group.map((r) => (
                <button
                  className="request-row"
                  key={r.id}
                  onClick={() => navigate("/requests/" + r.id)}
                >
                  <IdentityIcon
                    kind={r.kind === "approval" ? "service" : "agent"}
                    label={r.kind}
                  />
                  <div className="request-text">
                    <strong>{r.title}</strong>
                    <p>{r.body}</p>
                    {group.length > 1 ? (
                      <small>
                        Requester <IdentityReference id={r.requester_id} /> ·{" "}
                        <span title={r.id}>Request {r.id.slice(-8)}</span>
                      </small>
                    ) : null}
                  </div>
                  <Badge>{r.kind}</Badge>
                  <Status value={r.response?.outcome || r.state} />
                  <time title={r.created_at}>{relative(r.created_at)}</time>
                  <Icon name="chevron" size={15} />
                </button>
              ))}
            </section>
          ))}
        </div>
      </QueryState>
      <small>
        {q.items.length} permitted records loaded. Historical results use
        current permissions.
      </small>
      {compose ? (
        <Compose mode="question" onClose={() => setCompose(false)} />
      ) : null}
    </>
  );
}
function RequestDetail({ id }: { id: string }) {
  const request = useResource<Request>("/requests/" + id),
    caps = useCapabilities(undefined, id),
    publish = useCapabilities(),
    me = useSession();
  const [answer, setAnswer] = useDraft("response:" + id);
  const [confirm, setConfirm] = useState(""),
    [clarify, setClarify] = useState(false),
    [validation, setValidation] = useState<Error | null>(null),
    [sharing, setSharing] = useState(false),
    [shareNow, setShareNow] = useState(false),
    [structured, setStructured] = useState(false);
  const op = useOperation("response:" + id);
  const pending = useCollection<Request>("requests", "", { state: "open" });
  const next = pending.items.find((item) => item.id !== id);
  const conversation = useResource<Conversation>(
    request.data
      ? "/conversations/" + request.data.conversation_id
      : "/conversations",
    !!request.data,
  );
  useEffect(() => {
    if (caps.error || (caps.data && !caps.data.operations.respond))
      setAnswer("");
  }, [caps.error, caps.data, setAnswer]);
  if (request.isPending || caps.isPending)
    return <Loading label="Opening request" />;
  if (request.error || caps.error)
    return (
      <ErrorView
        error={request.error || caps.error}
        retry={() => {
          void request.refetch();
          void caps.refetch();
        }}
      />
    );
  const r = request.data!,
    c = caps.data!;
  async function respond(outcome: string) {
    setValidation(null);
    if (r.kind === "question" && (!answer.trim() || bytes(answer) > 32768)) {
      setValidation(Error("Enter an answer within 32,768 UTF-8 bytes."));
      return;
    }
    let value: unknown = answer || undefined;
    try {
      if (structured && r.kind === "question" && !r.choices?.length)
        value = JSON.parse(answer);
    } catch {
      setValidation(
        Error("Enter valid JSON, or turn off structured answer mode."),
      );
      return;
    }
    const result = await op.run("/requests/" + id + "/responses", {
      outcome,
      answer: value,
      version: r.version,
    });
    if (result) setAnswer("");
  }
  const requester = conversation.data?.participants.find(
    (p) => p.id === r.requester_id,
  );
  const stale = c.request_version !== r.version;
  return (
    <>
      <PageHeader
        title="Request"
        actions={
          <>
            <Button onClick={() => navigate("/inbox")}>Back to inbox</Button>
            {next ? (
              <Button onClick={() => navigate("/requests/" + next.id)}>
                Next open request
              </Button>
            ) : null}
          </>
        }
      />
      <ErrorView error={op.error || validation} />
      <section className="request-hero">
        <div className="actions">
          <Badge>{r.kind}</Badge>
          <Status value={r.response?.outcome || r.state} />
          <small>Version {r.version}</small>
        </div>
        <h1>{r.title}</h1>
        <div className="prose">{r.body}</div>
        <dl className="detail-list">
          <dt>Requested by</dt>
          <dd>
            {requester?.name || r.requester_id}
            {r.requester_instance_id ? (
              <>
                <br />
                <code>{r.requester_instance_id}</code>
              </>
            ) : null}
          </dd>
          <dt>Created</dt>
          <dd>
            <DateValue value={r.created_at} />
          </dd>
          <dt>Deadline</dt>
          <dd>
            <DateValue value={r.expires_at} />
          </dd>
          <dt>Reference</dt>
          <dd>
            <code>{r.id}</code>
          </dd>
        </dl>
        <div className="actions">
          <Button
            icon="conversations"
            onClick={() => navigate("/conversations/" + r.conversation_id)}
          >
            Open conversation
          </Button>
          {requester ? (
            <Button onClick={() => setClarify(true)}>
              Ask for clarification
            </Button>
          ) : null}
        </div>
      </section>
      {r.action ? (
        <section className="approval">
          <h3>
            <Icon name="permissions" />
            Proposed action
          </h3>
          <dl className="detail-list">
            <dt>Action</dt>
            <dd>{r.action.name}</dd>
            <dt>Executor</dt>
            <dd>
              <code>{r.action.executor_id}</code>
            </dd>
            <dt>Resource</dt>
            <dd>
              <code>{r.action.resource}</code>
            </dd>
            <dt>Resource version</dt>
            <dd>
              <code>{r.action.resource_version}</code>
            </dd>
            <dt>Operation key</dt>
            <dd>
              <code>{r.action.operation_key}</code>
            </dd>
          </dl>
          <h3>Exact arguments</h3>
          <pre>{readable(r.action.arguments)}</pre>
          <p className="help">
            A decision records permission for this exact proposal. Execution is
            reported separately by the authenticated runtime.
          </p>
        </section>
      ) : null}
      {stale ? (
        <ErrorView
          error={Error("The request changed. Refresh before deciding.")}
          retry={() => {
            void request.refetch();
            void caps.refetch();
          }}
        />
      ) : c.operations.respond ? (
        <Form
          onSubmit={() =>
            void respond(r.kind === "question" ? "answered" : "completed")
          }
        >
          <div className="response-form">
            {r.kind === "question" && r.choices?.length ? (
              <Field label="Your answer">
                <select
                  required
                  value={answer}
                  onChange={(e) => setAnswer(e.target.value)}
                >
                  <option value="">Choose an answer</option>
                  {r.choices.map((choice) => (
                    <option key={choice} value={choice}>
                      {choice}
                    </option>
                  ))}
                </select>
              </Field>
            ) : r.kind !== "approval" ? (
              <Field
                label={
                  r.kind === "question"
                    ? "Your answer"
                    : "Result or context (optional)"
                }
              >
                <textarea
                  rows={4}
                  value={answer}
                  onChange={(e) => setAnswer(e.target.value)}
                  required={r.kind === "question"}
                />
              </Field>
            ) : null}
            {r.kind === "question" && !r.choices?.length ? (
              <label className="inline-check">
                <input
                  type="checkbox"
                  checked={structured}
                  onChange={(e) => setStructured(e.target.checked)}
                />
                Answer as structured JSON
              </label>
            ) : null}
            <div className="response-buttons">
              {c.outcomes.map((outcome) => (
                <Button
                  key={outcome}
                  primary={["approved", "answered", "completed"].includes(
                    outcome,
                  )}
                  danger={["denied", "failed", "rejected"].includes(outcome)}
                  type="button"
                  disabled={
                    op.pending ||
                    r.state !== "open" ||
                    Date.parse(r.expires_at) <= Date.now()
                  }
                  onClick={() =>
                    r.kind === "approval"
                      ? setConfirm(outcome)
                      : void respond(outcome)
                  }
                >
                  {(
                    {
                      approved: "Approve action",
                      denied: "Deny action",
                      answered: "Send answer",
                      completed: "Mark completed",
                      failed: "Report failed",
                      rejected: "Reject task",
                    } as Record<string, string>
                  )[outcome] || outcome}
                </Button>
              ))}
              {r.kind === "question" &&
              r.shared_key &&
              publish.data?.operations.publish_answer ? (
                <Button
                  disabled={op.pending || !answer.trim()}
                  onClick={() => {
                    try {
                      if (structured) JSON.parse(answer);
                      setShareNow(true);
                    } catch {
                      setValidation(Error("Enter valid JSON before sharing."));
                    }
                  }}
                >
                  Answer and share…
                </Button>
              ) : null}
            </div>
            {["approved", "denied"].includes(confirm) ? (
              <section className="approval">
                <p>
                  {confirm === "approved" ? "Approve" : "Deny"} the exact action
                  shown above?
                </p>
                <div className="form-actions">
                  <Button type="button" onClick={() => setConfirm("")}>
                    Back
                  </Button>
                  <Button
                    primary
                    type="button"
                    disabled={op.pending}
                    onClick={() =>
                      void respond(confirm).then(() => setConfirm(""))
                    }
                  >
                    Confirm {confirm === "approved" ? "approval" : "denial"}
                  </Button>
                </div>
              </section>
            ) : null}
          </div>
        </Form>
      ) : r.state === "open" ? (
        <p className="help">
          This request is readable, but you are not currently eligible to
          respond.
        </p>
      ) : null}
      {r.response ? (
        <section className="section">
          <h2>Recorded outcome</h2>
          <Badge>{r.response.outcome}</Badge>
          <pre>
            {r.response.answer === undefined ? "" : readable(r.response.answer)}
          </pre>
          <small>
            By {r.resolved_by || "Authorized responder"} ·{" "}
            <DateValue value={r.resolved_at} />
          </small>
          {r.response.source_answer_id ? (
            <p className="help">
              Shared answer {r.response.source_answer_id} ·{" "}
              {r.response.reuse_method}. {r.response.reuse_reason}
              {r.response.source_invalidated
                ? " This source is no longer valid for future reuse. Historical results remain recorded."
                : ""}
            </p>
          ) : null}
          {r.kind === "question" && publish.data?.operations.publish_answer ? (
            <Button onClick={() => setSharing(true)}>
              Publish informational answer
            </Button>
          ) : null}
        </section>
      ) : null}
      <div className="actions">
        {c.operations.cancel ? (
          <Button
            danger
            disabled={op.pending}
            onClick={() => setConfirm("cancel")}
          >
            Cancel request
          </Button>
        ) : null}
        {c.operations.supersede ? (
          <Button disabled={op.pending} onClick={() => setConfirm("supersede")}>
            Mark superseded
          </Button>
        ) : null}
      </div>
      {["cancel", "supersede"].includes(confirm) ? (
        <section className="approval">
          <p>
            {confirm === "cancel" ? "Cancel" : "Supersede"} this open request?
            This does not stop an external action.
          </p>
          <div className="form-actions">
            <Button onClick={() => setConfirm("")}>Keep open</Button>
            <Button
              danger
              primary
              disabled={op.pending}
              onClick={() =>
                void op
                  .run("/requests/" + id + "/" + confirm, {
                    version: r.version,
                  })
                  .then((v) => {
                    if (v) setConfirm("");
                  })
              }
            >
              Confirm
            </Button>
          </div>
        </section>
      ) : null}
      {r.kind === "question" && r.state === "open" && c.operations.respond ? (
        <Suggestions request={r} />
      ) : null}
      <RequestEvidence request={r} />
      {clarify && requester ? (
        <Compose
          initial={[{ kind: requester.kind, id: requester.id }]}
          conversation={r.conversation_id}
          onClose={() => setClarify(false)}
        />
      ) : null}
      {shareNow ? (
        <AnswerAndShare
          request={r}
          answer={structured ? JSON.parse(answer) : answer}
          onClose={() => setShareNow(false)}
          onAccepted={() => {
            setAnswer("");
            setShareNow(false);
          }}
        />
      ) : null}
      {sharing ? (
        <Publish request={r} onClose={() => setSharing(false)} />
      ) : null}
      <small>
        Signed in as {me.person.name}. Current policy is checked for every
        operation.
      </small>
    </>
  );
}
function RequestEvidence({ request: r }: { request: Request }) {
  const trace = useResource<{ evidence: string }>(
    "/requests/" + r.id + "/trace",
  );
  const deliveries = useCollection<Record<string, unknown>>("deliveries", "", {
      request_id: r.id,
    }),
    executions = useCollection<Record<string, unknown>>("executions", "", {
      request_id: r.id,
    });
  return (
    <section className="section">
      <h2>Delivery and execution evidence</h2>
      <p>
        {trace.data?.evidence ||
          "Transport, recorded decisions and execution reports are separate."}
      </p>
      <ErrorView error={trace.error} />
      <h3>Deliveries</h3>
      <QueryState query={deliveries} empty="No readable delivery evidence">
        {deliveries.items.map((d) => (
          <div className="timeline-row" key={String(d.id)}>
            <Status value={String(d.state)} />
            <small>
              {" "}
              · {String(d.recipient_id)} · {String(d.attempts)} attempts
            </small>
            <Details value={d} label="Route and attempt evidence" />
          </div>
        ))}
      </QueryState>
      <h3>Execution reports</h3>
      <QueryState query={executions} empty="No execution report available">
        {executions.items.map((e) => (
          <div className="timeline-row" key={String(e.id)}>
            <Status value={String(e.state)} />
            <Details value={e} label="Authenticated executor report" />
          </div>
        ))}
      </QueryState>
    </section>
  );
}
function Suggestions({ request: r }: { request: Request }) {
  const op = useOperation();
  const [suggestions, setSuggestions] = useState<
    { id: string; answer: Record<string, unknown>; reason: string }[] | null
  >(null);
  return (
    <section className="section">
      <h3>Reusable information</h3>
      <p>
        Review a candidate's source, audience and validity before accepting it.
      </p>
      <Button
        disabled={op.pending}
        onClick={() =>
          void op
            .run<{
              proposals: {
                id: string;
                answer: Record<string, unknown>;
                reason: string;
              }[];
            }>("/requests/" + r.id + "/suggestions", {})
            .then((v) => {
              if (v) setSuggestions(v.proposals);
            })
        }
      >
        Find shared answer suggestions
      </Button>
      <ErrorView error={op.error} />
      {suggestions?.length === 0 ? (
        <p>No eligible suggestion found. You can answer normally.</p>
      ) : null}
      {suggestions?.map((s) => (
        <div className="approval" key={s.id}>
          <p>{s.reason}</p>
          <Details
            value={s.answer}
            label="Answer, source, audience and expiry"
          />
          <div className="actions">
            <Button
              primary
              disabled={op.pending}
              onClick={() =>
                void op
                  .run("/answer-proposals/" + s.id, { accept: true })
                  .then((v) => {
                    if (v) setSuggestions(null);
                  })
              }
            >
              Accept this answer
            </Button>
            <Button
              disabled={op.pending}
              onClick={() =>
                void op
                  .run("/answer-proposals/" + s.id, { accept: false })
                  .then((v) => {
                    if (v)
                      setSuggestions(
                        (x) => x?.filter((p) => p.id !== s.id) || null,
                      );
                  })
              }
            >
              Keep separate
            </Button>
          </div>
        </div>
      ))}
    </section>
  );
}
function Publish({
  request: r,
  onClose,
}: {
  request: Request;
  onClose: () => void;
}) {
  const [audience, setAudience] = useState<Target[]>([]),
    [until, setUntil] = useState(""),
    [key, setKey] = useState(r.shared_key),
    op = useOperation();
  return (
    <section className="approval">
      <h3>Publish informational answer</h3>
      <Form
        onSubmit={() =>
          void op
            .run("/answers", {
              request_id: r.id,
              key,
              audience: audience.map((t) => t.id),
              valid_until: until ? new Date(until).toISOString() : undefined,
            })
            .then((v) => {
              if (v) onClose();
            })
        }
      >
        <p className="help">
          This explicitly allows reuse for compatible pending and future
          questions within the selected audience and validity period. It never
          grants approval authority.
        </p>
        <Field label="Scope key">
          <input
            required
            value={key}
            onChange={(e) => setKey(e.target.value)}
          />
        </Field>
        <h3>Explicit audience</h3>
        <TargetPicker
          value={audience}
          onChange={setAudience}
          multiple
          allowGroups={false}
          allowInstances={false}
        />
        <Field
          label="Valid until"
          hint="Defaults to 24 hours, maximum 90 days."
        >
          <input
            type="datetime-local"
            value={until}
            onChange={(e) => setUntil(e.target.value)}
          />
        </Field>
        <ErrorView error={op.error} />
        <div className="form-actions">
          <Button onClick={onClose}>Cancel</Button>
          <Button
            primary
            type="submit"
            disabled={op.pending || !audience.length}
          >
            Publish answer
          </Button>
        </div>
      </Form>
    </section>
  );
}

function AnswerAndShare({
  request: r,
  answer,
  onClose,
  onAccepted,
}: {
  request: Request;
  answer: unknown;
  onClose: () => void;
  onAccepted: () => void;
}) {
  const [audience, setAudience] = useState<Target[]>([]);
  const op = useOperation("answer-share:" + r.id);
  return (
    <Modal title="Answer and share information" wide onClose={onClose}>
      <Form
        onSubmit={() =>
          void op
            .run("/requests/" + r.id + "/answer-and-share", {
              outcome: "answered",
              answer,
              version: r.version,
              audience: audience.map((t) => t.id),
            })
            .then((v) => {
              if (v) onAccepted();
            })
        }
      >
        <p>
          This records your answer and publishes it for the explicit audience
          below. Compatible pending questions may resolve using it. The
          publication lasts 24 hours. Approvals and tasks are never reused.
        </p>
        <dl className="detail-list">
          <dt>Scope key</dt>
          <dd>{r.shared_key}</dd>
          <dt>Exact answer</dt>
          <dd className="prose">{readable(answer)}</dd>
        </dl>
        <TargetPicker
          value={audience}
          onChange={setAudience}
          multiple
          allowGroups={false}
          allowInstances={false}
        />
        <ErrorView error={op.error} />
        <div className="form-actions">
          <Button onClick={onClose}>Keep private</Button>
          <Button
            primary
            type="submit"
            disabled={op.pending || !audience.length}
          >
            Confirm answer and publication
          </Button>
        </div>
      </Form>
    </Modal>
  );
}
