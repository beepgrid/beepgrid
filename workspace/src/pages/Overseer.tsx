import { useEffect, useRef, useState } from "react";
import {
  useCollection,
  useDraft,
  useOperation,
  useResource,
  useSession,
} from "../data";
import type { AssistanceSettings, Group, Plan } from "../contracts";
import { bytes, readable } from "../client";
import { navigate } from "../navigation";
import { OverseerMark, assistantName } from "../OverseerEntry";
import {
  Badge,
  Button,
  DateValue,
  ErrorView,
  Field,
  Form,
  IdentityIcon,
  Icon,
  Loading,
  RequestStatus,
  unavailable,
} from "../ui";

function CoordinatorAgents({ turn }: { turn: Plan }) {
  const ids = [
    ...new Set([
      ...(turn.instructions || []).map((i) => i.agent_id),
      ...(turn.dispatches || []).map((d) => d.agent_id),
    ]),
  ];
  const [expanded, setExpanded] = useState(false);
  if (!ids.length) return null;
  return (
    <div className="coordinator-instructions">
      {(expanded ? ids : ids.slice(0, 5)).map((id) => {
        const dispatch = turn.dispatches?.find((d) => d.agent_id === id);
        const reply = turn.replies?.find((r) => r.id === dispatch?.request_id);
        const agent = turn.sources.find((s) => s.agent.id === id)?.agent;
        return (
          <div key={id}>
            <IdentityIcon
              kind="agent"
              id={id}
              label={agent?.name}
              icon={String(agent?.metadata?.appearance_icon || "")}
            />
            <div className="coordinator-agent-body">
              <header>
                <strong>{agent?.name || "Agent"}</strong>
                {reply ? (
                  <RequestStatus request={reply} />
                ) : dispatch ? (
                  <Badge>Sent</Badge>
                ) : (
                  <Badge>Proposed</Badge>
                )}
                {dispatch ? (
                  <Button
                    icon="arrow"
                    onClick={() =>
                      navigate("/conversations/" + dispatch.conversation_id)
                    }
                  >
                    Open conversation
                  </Button>
                ) : null}
              </header>
              <p>
                {turn.instructions?.find((i) => i.agent_id === id)?.body ||
                  turn.body}
              </p>
              {reply?.response ? (
                <p className="coordinator-agent-reply">
                  {readable(reply.response.answer ?? reply.response.outcome)}
                </p>
              ) : null}
            </div>
          </div>
        );
      })}
      {ids.length > 5 ? (
        <Button onClick={() => setExpanded(!expanded)}>
          {expanded
            ? "Show fewer Agents"
            : `Show ${ids.length - 5} more Agents`}
        </Button>
      ) : null}
    </div>
  );
}

export default function Overseer() {
  const me = useSession();
  const settings = useResource<AssistanceSettings>("/assistance/settings");
  const enabled = settings.data?.overseer === true;
  const history = useCollection<Plan>("overseer_plans", "", {}, enabled);
  const groups = useCollection<Group>("groups", "", {}, enabled);
  const [prompt, setPrompt] = useDraft("overseer:prompt");
  const [group, setGroup] = useDraft("overseer:group");
  const [topic, setTopic] = useDraft("overseer:topic");
  const freshTopic = topic === "new";
  const setFreshTopic = (fresh: boolean) => setTopic(fresh ? "new" : "");
  const [notice, setNotice] = useState("");
  const [review, setReview] = useState(false);
  const [clock, setClock] = useState(Date.now());
  const planning = useOperation("overseer");
  const op = useOperation();
  const busy = planning.pending || op.pending;
  const scroll = useRef<HTMLDivElement>(null);
  const promptControl = useRef<HTMLTextAreaElement>(null);
  const initialized = useRef(false);
  const pinned = useRef(true);
  // The API returns newest first. Paint chronological turns, keeping older pages explicit.
  const turns = history.items.slice().reverse();
  const latest = history.items[0];
  const nextExpiry = Math.min(
    ...history.items
      .filter((p) => p.requires_confirmation && !p.dispatches?.length)
      .map((p) => Date.parse(p.expires_at))
      .filter((at) => at > clock),
  );
  useEffect(() => {
    if (!Number.isFinite(nextExpiry)) return;
    const tick = setTimeout(
      () => setClock(Date.now()),
      Math.max(0, nextExpiry - Date.now() + 10),
    );
    return () => clearTimeout(tick);
  }, [nextExpiry]);
  useEffect(() => {
    if (settings.data?.overseer === false || unavailable(history.error)) {
      initialized.current = false;
      setNotice("");
      op.forget();
      planning.forget();
      setPrompt("");
    }
  }, [
    settings.data?.overseer,
    history.error,
    op.forget,
    planning.forget,
    setPrompt,
  ]);
  useEffect(() => {
    if (
      enabled &&
      !history.isPending &&
      !history.error &&
      (!initialized.current || pinned.current)
    ) {
      if (scroll.current)
        scroll.current.scrollTop = scroll.current.scrollHeight;
      initialized.current = true;
    }
  }, [
    enabled,
    history.isPending,
    history.error,
    turns.length,
    latest?.reply_summary,
    freshTopic,
  ]);
  async function ask() {
    if (!prompt.trim()) return;
    if (bytes(prompt) > 4000) {
      setNotice("Use at most 4,000 UTF-8 bytes.");
      return;
    }
    // Changing fleet scope starts a new topic so follow-ups do not silently mix audiences.
    const previous =
      !freshTopic && latest && (latest.target?.id || "") === group
        ? latest.id
        : undefined;
    const retry = planning.previous<{
      prompt: string;
      target?: { kind: string; id: string };
      previous_plan_id?: string;
    }>(review ? "/fleet/plan" : "/fleet/coordinate");
    const body =
      retry?.prompt === prompt && (retry.target?.id || "") === group
        ? retry
        : {
            prompt,
            ...(group ? { target: { kind: "group", id: group } } : {}),
            ...(previous ? { previous_plan_id: previous } : {}),
          };
    const accepted = await planning.run<Plan>(
      review ? "/fleet/plan" : "/fleet/coordinate",
      body,
    );
    if (accepted) {
      setPrompt("");
      setFreshTopic(false);
      setNotice("");
      pinned.current = true;
    }
  }
  return (
    <section
      className="chat overseer-chat"
      aria-label={"Private " + assistantName + " conversation"}
    >
      <header className="chat-head coordinator-head">
        <OverseerMark />
        <div className="chat-heading">
          <strong>{assistantName}</strong>
          <p>
            <Icon name="lock" size={12} /> Private to you
          </p>
        </div>
        <details className="coordinator-info">
          <summary aria-label="About Coordinator">
            <Icon name="info" size={17} />
          </summary>
          <p>
            Coordinator sends permitted communications as you and brings replies
            together. Business actions still need their own approvals.
          </p>
        </details>
        {enabled ? (
          <Button
            disabled={busy || freshTopic}
            onClick={() => {
              planning.forget();
              setFreshTopic(true);
              setNotice("");
              pinned.current = true;
              promptControl.current?.focus();
            }}
          >
            New topic
          </Button>
        ) : null}
      </header>
      <div
        ref={scroll}
        onScroll={() => {
          const el = scroll.current;
          if (el)
            pinned.current =
              el.scrollHeight - el.scrollTop - el.clientHeight < 80;
        }}
        className={`messages coordinator-messages ${!enabled || !turns.length ? "coordinator-intro" : ""}`}
      >
        {settings.isPending ? (
          <Loading label={"Checking " + assistantName + " availability"} />
        ) : settings.error ? (
          <ErrorView
            error={settings.error}
            retry={() => void settings.refetch()}
          />
        ) : !enabled ? (
          <div className="coordinator-welcome">
            <OverseerMark />
            <span className="coordinator-eyebrow">Your built-in Agent</span>
            <h2>
              One place to coordinate
              <br />
              your Agents.
            </h2>
            <p>
              Bring together updates, find the right Agents and prepare work for
              your fleet.
            </p>
            <div className="coordinator-setup">
              {me.admin ? (
                <Button
                  icon="arrow"
                  onClick={() => navigate("/settings/assistance")}
                >
                  Set up Coordinator
                </Button>
              ) : (
                <small>
                  Ask a workspace administrator to enable Coordinator.
                </small>
              )}
            </div>
          </div>
        ) : history.error ? (
          <ErrorView
            error={history.error}
            retry={() => void history.refetch()}
          />
        ) : history.isPending ? (
          <Loading label="Opening your private history" />
        ) : (
          <>
            {history.hasNextPage ? (
              <Button
                disabled={history.isFetchingNextPage}
                onClick={() => {
                  pinned.current = false;
                  void history.fetchNextPage();
                }}
              >
                {history.isFetchingNextPage
                  ? "Loading history"
                  : "Load earlier messages"}
              </Button>
            ) : null}
            {!turns.length ? (
              <div className="coordinator-welcome">
                <OverseerMark />
                <span className="coordinator-eyebrow">Your built-in Agent</span>
                <h2>What would you like to do?</h2>
                <p>
                  Bring together updates, find the right Agents and prepare work
                  for your fleet.
                </p>
                <div className="coordinator-starters">
                  {[
                    [
                      "conversations",
                      "Check release blockers",
                      "Find out which Agents are waiting on release blockers and summarize what needs my attention.",
                    ],
                    [
                      "task",
                      "Prepare tasks for my Agents",
                      "Help me split the next release into tasks and find the right Agents for each one.",
                    ],
                  ].map(([icon, label, text]) => (
                    <button
                      key={label}
                      disabled={busy}
                      onClick={() => {
                        setPrompt(text);
                        promptControl.current?.focus();
                      }}
                    >
                      <Icon name={icon} size={17} />
                      <span>{label}</span>
                      <Icon name="arrow" size={15} />
                    </button>
                  ))}
                </div>
              </div>
            ) : null}
            <div className="coordinator-thread">
              {turns.map((turn, index) => (
                <div className="overseer-turn" key={turn.id}>
                  {!turn.previous_plan_id || index === 0 ? (
                    <div
                      className="coordinator-topic"
                      role="separator"
                      aria-label={
                        !turn.previous_plan_id ? "New topic" : "Earlier topic"
                      }
                    >
                      <span>
                        {!turn.previous_plan_id ? "New topic" : "Earlier topic"}
                      </span>
                      <DateValue value={turn.created_at} />
                    </div>
                  ) : null}
                  <article className="message">
                    <IdentityIcon
                      kind="person"
                      id={me.person.id}
                      label={me.person.name}
                    />
                    <div className="message-body">
                      <div className="message-meta">
                        <strong>You</strong>
                        <DateValue value={turn.created_at} />
                      </div>
                      <p>{turn.prompt}</p>
                    </div>
                  </article>
                  <article className="message">
                    <OverseerMark />
                    <div className="message-body">
                      <div className="message-meta">
                        <strong>{assistantName}</strong>
                        <Badge
                          tone={turn.dispatches?.length ? "good" : "accent"}
                        >
                          {turn.operation === "status"
                            ? "Interpretation"
                            : turn.dispatches?.length
                              ? "Sent"
                              : "Proposal"}
                        </Badge>
                      </div>
                      <p>{turn.summary}</p>
                      <CoordinatorAgents turn={turn} />
                      {turn.summary_error ? (
                        <small>{turn.summary_error}</small>
                      ) : null}
                      {turn.operation !== "status" &&
                      (!turn.dispatches?.length ||
                        !turn.instructions?.length) ? (
                        <section className="approval">
                          <h3>
                            {turn.dispatches?.length
                              ? "Accepted instruction"
                              : "Review exact instruction"}
                          </h3>
                          <p>
                            <strong>{turn.operation}</strong> to{" "}
                            {turn.sources.map((s) => s.agent.name).join(", ")}
                          </p>
                          {turn.body ? <pre>{turn.body}</pre> : null}
                          {!turn.dispatches?.length ? (
                            <>
                              <small>
                                {Date.parse(turn.expires_at) <= clock ? (
                                  "This proposal expired. Ask again to prepare a fresh one."
                                ) : (
                                  <>
                                    Expires{" "}
                                    <DateValue value={turn.expires_at} />
                                  </>
                                )}
                              </small>
                              <div className="form-actions">
                                <Button
                                  primary
                                  disabled={
                                    busy ||
                                    Date.parse(turn.expires_at) <= clock ||
                                    !turn.requires_confirmation
                                  }
                                  onClick={() =>
                                    void op
                                      .run<Plan>("/fleet/command", {
                                        plan_id: turn.id,
                                      })
                                      .then((p) => {
                                        if (p)
                                          setNotice(
                                            "Instruction accepted. Delivery and replies are tracked separately.",
                                          );
                                      })
                                  }
                                >
                                  Confirm exact instruction
                                </Button>
                              </div>
                            </>
                          ) : null}
                        </section>
                      ) : null}
                      {turn.replies?.length &&
                      (!turn.automatic || turn.summary_error) ? (
                        <Button
                          disabled={
                            busy ||
                            !turn.replies.some(
                              (r) =>
                                r.state !== "open" &&
                                r.state !== "clarification",
                            )
                          }
                          onClick={() =>
                            void op
                              .run<Plan>(
                                "/fleet/plans/" + turn.id + "/summary",
                                {},
                              )
                              .then((p) => {
                                if (p) setNotice("");
                              })
                          }
                        >
                          {turn.summary_error
                            ? "Retry summary"
                            : "Summarize replies"}
                        </Button>
                      ) : null}
                      {turn.automatic &&
                      turn.replies?.length &&
                      !turn.reply_summary &&
                      !turn.summary_error ? (
                        <small
                          className="muted coordinator-waiting"
                          role="status"
                        >
                          {turn.replies.some(
                            (r) =>
                              r.state === "open" || r.state === "clarification",
                          )
                            ? "Waiting for replies"
                            : "Bringing replies together"}
                          <span className="waiting-dots" aria-hidden="true">
                            <i>.</i>
                            <i>.</i>
                            <i>.</i>
                          </span>
                        </small>
                      ) : null}
                      {turn.reply_summary ? (
                        <section className="section">
                          <h3>Combined reply</h3>
                          <p>{turn.reply_summary}</p>
                        </section>
                      ) : null}
                    </div>
                  </article>
                </div>
              ))}
              {freshTopic ? (
                <div className="coordinator-topic-draft" role="status">
                  <div>
                    <strong>New topic</strong>
                  </div>
                  {latest && (latest.target?.id || "") === group ? (
                    <Button
                      onClick={() => {
                        setFreshTopic(false);
                        promptControl.current?.focus();
                      }}
                    >
                      Continue previous topic
                    </Button>
                  ) : null}
                </div>
              ) : null}
            </div>
          </>
        )}
      </div>
      {enabled ? (
        <div className="composer coordinator-composer">
          <ErrorView error={planning.error} />
          <ErrorView error={op.error} />
          {notice ? (
            <p className="notice" role="status">
              {notice}
            </p>
          ) : null}
          {history.error || history.isPending ? (
            <p className="help">
              Restore current history before continuing this conversation.
            </p>
          ) : (
            <Form onSubmit={() => void ask()}>
              <Field
                label={
                  freshTopic
                    ? "New topic for " + assistantName
                    : "Message " + assistantName
                }
              >
                <textarea
                  ref={promptControl}
                  rows={2}
                  required
                  maxLength={4000}
                  disabled={busy}
                  value={prompt}
                  onChange={(e) => setPrompt(e.target.value)}
                  placeholder="Message Coordinator…"
                />
              </Field>
              <div className="actions coordinator-controls">
                <select
                  aria-label="Agents"
                  value={group}
                  disabled={busy}
                  onChange={(e) => {
                    planning.forget();
                    setGroup(e.target.value);
                    setFreshTopic(true);
                  }}
                >
                  <option value="">All Agents</option>
                  {group && !groups.items.some((g) => g.id === group) ? (
                    <option disabled value={group}>
                      Selected Agent role
                    </option>
                  ) : null}
                  {groups.items.map((g) => (
                    <option key={g.id} value={g.id}>
                      {g.name}
                    </option>
                  ))}
                </select>
                {groups.hasNextPage ? (
                  <Button
                    disabled={groups.isFetchingNextPage}
                    onClick={() => void groups.fetchNextPage()}
                  >
                    More roles
                  </Button>
                ) : null}
                <label className="coordinator-review">
                  <input
                    type="checkbox"
                    checked={review}
                    onChange={(e) => setReview(e.target.checked)}
                  />
                  Review before sending
                </label>
                <Button
                  primary
                  icon="arrow"
                  type="submit"
                  disabled={busy || !prompt.trim()}
                >
                  {planning.pending
                    ? "Preparing response"
                    : "Send to " + assistantName}
                </Button>
              </div>
              <ErrorView error={groups.error} />
              {planning.pending ? (
                <Loading label="Reviewing permitted reports and recent context" />
              ) : null}
            </Form>
          )}
        </div>
      ) : null}
    </section>
  );
}
