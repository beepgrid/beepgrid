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
  Details,
  Empty,
  ErrorView,
  Field,
  Form,
  IdentityIcon,
  Loading,
  unavailable,
} from "../ui";

function CoordinatorResults({ turn }: { turn: Plan }) {
  const [open, setOpen] = useState(false);
  const count = turn.dispatches?.length || 0;
  const settled =
    turn.replies?.filter(
      (r) => r.state !== "open" && r.state !== "clarification",
    ).length || 0;
  const results = () => (
    <>
      {turn.dispatches?.map((d) => (
        <div className="actions" key={d.agent_id}>
          <strong>
            {turn.sources.find((s) => s.agent.id === d.agent_id)?.agent.name ||
              d.agent_id}
          </strong>
          <Button
            onClick={() => navigate("/conversations/" + d.conversation_id)}
          >
            Conversation
          </Button>
          {d.request_id ? (
            <Button onClick={() => navigate("/requests/" + d.request_id)}>
              Request
            </Button>
          ) : (
            <Badge>Message accepted</Badge>
          )}
        </div>
      ))}
      {turn.replies?.map((r) => (
        <section className="section" key={r.id}>
          <Badge>{r.state}</Badge>{" "}
          <strong>
            {turn.sources.find(
              (s) =>
                turn.dispatches.find((d) => d.request_id === r.id)?.agent_id ===
                s.agent.id,
            )?.agent.name || r.title}
          </strong>
          <p>
            {r.response
              ? readable(r.response.answer ?? r.response.outcome)
              : "Awaiting an authorized reply"}
          </p>
          {r.resolved_at ? (
            <small>
              Resolved <DateValue value={r.resolved_at} />
            </small>
          ) : null}
        </section>
      ))}
    </>
  );
  if (count <= 5) return results();
  return (
    <details
      className="details"
      onToggle={(e) => setOpen(e.currentTarget.open)}
    >
      <summary>
        {count} agents ·{" "}
        {turn.replies?.length
          ? `${settled} of ${turn.replies.length} results settled`
          : "messages accepted"}{" "}
        · Individual results
      </summary>
      {open ? results() : null}
    </details>
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
  const [clock, setClock] = useState(Date.now());
  const planning = useOperation("overseer");
  const op = useOperation();
  const busy = planning.pending || op.pending;
  const scroll = useRef<HTMLDivElement>(null);
  const scopeControl = useRef<HTMLDetailsElement>(null);
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
  }, [enabled, history.isPending, history.error, turns.length]);
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
    }>("/fleet/plan");
    const body =
      retry?.prompt === prompt && (retry.target?.id || "") === group
        ? retry
        : {
            prompt,
            ...(group ? { target: { kind: "group", id: group } } : {}),
            ...(previous ? { previous_plan_id: previous } : {}),
          };
    const accepted = await planning.run<Plan>("/fleet/plan", body);
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
      <header className="chat-head">
        <OverseerMark />
        <div className="chat-heading">
          <strong>{assistantName}</strong>
          <p>Private to {me.person.name} · Uses your current permissions</p>
        </div>
        <Badge>Built-in assistant</Badge>
        <Button
          disabled={busy}
          onClick={() => {
            planning.forget();
            setFreshTopic(true);
            setNotice(
              "Your next message starts a new topic. Saved history stays here.",
            );
          }}
        >
          New topic
        </Button>
      </header>
      <div
        className="messages"
        ref={scroll}
        onScroll={() => {
          const el = scroll.current;
          if (el)
            pinned.current =
              el.scrollHeight - el.scrollTop - el.clientHeight < 80;
        }}
      >
        <p className="help">
          {assistantName} helps you understand and coordinate permitted agents.
          Every outgoing message or request needs your confirmation. Group
          conversations communicate directly with their participants.
        </p>
        {settings.isPending ? (
          <Loading label={"Checking " + assistantName + " availability"} />
        ) : settings.error ? (
          <ErrorView
            error={settings.error}
            retry={() => void settings.refetch()}
          />
        ) : !enabled ? (
          <Empty
            title={
              settings.data?.overseer_configured
                ? assistantName + " is turned off"
                : assistantName + " is not configured"
            }
            icon="spark"
          >
            Your administrator can configure and enable this optional assistant
            in Settings. Direct agent and group communication remains available.
          </Empty>
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
              <Empty title="What would you like to know?" icon="spark">
                Ask about current reports, request an update, or prepare an
                instruction for review.
              </Empty>
            ) : null}
            {turns.map((turn) => (
              <div className="overseer-turn" key={turn.id}>
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
                      <Badge>
                        {turn.operation === "status"
                          ? "Interpretation"
                          : turn.dispatches?.length
                            ? "Confirmed"
                            : "Proposal"}
                      </Badge>
                    </div>
                    <p>{turn.summary}</p>
                    <Details
                      defer
                      label={`Sources and freshness (${turn.sources.length})`}
                      value={turn.sources}
                    />
                    {turn.operation !== "status" ? (
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
                        <pre>{turn.body}</pre>
                        {!turn.dispatches?.length ? (
                          <>
                            <small>
                              {Date.parse(turn.expires_at) <= clock ? (
                                "This proposal expired. Ask again to prepare a fresh one."
                              ) : (
                                <>
                                  Expires <DateValue value={turn.expires_at} />
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
                    <CoordinatorResults turn={turn} />
                    {turn.replies?.length ? (
                      <Button
                        disabled={
                          busy ||
                          !turn.replies.some(
                            (r) =>
                              r.state !== "open" && r.state !== "clarification",
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
                        Summarize replies
                      </Button>
                    ) : null}
                    {turn.reply_summary ? (
                      <section className="section">
                        <h3>Assistant summary</h3>
                        <p>{turn.reply_summary}</p>
                      </section>
                    ) : null}
                  </div>
                </article>
              </div>
            ))}
          </>
        )}
      </div>
      <div className="composer">
        <ErrorView error={planning.error} />
        <ErrorView error={op.error} />
        {notice ? (
          <p className="notice" role="status">
            {notice}
          </p>
        ) : null}
        {!enabled ? (
          <Button onClick={() => navigate("/settings/assistance")}>
            Assistance settings
          </Button>
        ) : history.error || history.isPending ? (
          <p className="help">
            Restore current history before continuing this conversation.
          </p>
        ) : (
          <Form onSubmit={() => void ask()}>
            <details className="details" ref={scopeControl}>
              <summary>
                Agents:{" "}
                {group
                  ? groups.items.find((g) => g.id === group)?.name ||
                    "Selected group"
                  : "All permitted (up to 100)"}
              </summary>
              <Field label="Agent scope">
                <select
                  value={group}
                  disabled={busy}
                  onChange={(e) => {
                    planning.forget();
                    setGroup(e.target.value);
                    if (scopeControl.current) scopeControl.current.open = false;
                    setFreshTopic(true);
                  }}
                >
                  <option value="">All permitted agents (up to 100)</option>
                  {group && !groups.items.some((g) => g.id === group) ? (
                    <option disabled value={group}>
                      Saved group (not loaded)
                    </option>
                  ) : null}
                  {groups.items.map((g) => (
                    <option key={g.id} value={g.id}>
                      {g.name}
                    </option>
                  ))}
                </select>
              </Field>
              {groups.hasNextPage ? (
                <Button
                  disabled={groups.isFetchingNextPage}
                  onClick={() => void groups.fetchNextPage()}
                >
                  Load more groups
                </Button>
              ) : null}
              <ErrorView error={groups.error} />
            </details>
            <Field
              label={
                freshTopic
                  ? "New topic for " + assistantName
                  : "Message " + assistantName
              }
            >
              <textarea
                rows={2}
                required
                maxLength={4000}
                disabled={busy}
                value={prompt}
                onChange={(e) => setPrompt(e.target.value)}
                placeholder="Ask about your agents or prepare an instruction…"
              />
            </Field>
            <div className="actions">
              <small>Recent context · Your current permissions</small>
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
            {planning.pending ? (
              <Loading label="Reviewing permitted reports and recent context" />
            ) : null}
          </Form>
        )}
      </div>
    </section>
  );
}
