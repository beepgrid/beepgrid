import { useEffect, useRef, useState } from "react";
import { api, type Group, type RequestItem } from "./api";

export type AssistanceSettings = {
  automatic_answers: boolean;
  overseer: boolean;
  version: number;
  matcher_configured: boolean;
  overseer_configured: boolean;
};
export const disabledAssistance: AssistanceSettings = {
  automatic_answers: false,
  overseer: false,
  version: 0,
  matcher_configured: false,
  overseer_configured: false,
};
type Plan = {
  reply_summary?: string;
  reply_digest?: string;
  id: string;
  operation: string;
  prompt: string;
  summary: string;
  body?: string;
  context_agent_ids: string[];
  requires_confirmation: boolean;
  created_at: string;
  expires_at: string;
  sources: {
    agent: { id: string; name: string };
    fresh: boolean;
    instances: { progress: Record<string, unknown>; last_seen: string }[];
  }[];
  dispatches: {
    agent_id: string;
    conversation_id: string;
    request_id?: string;
  }[];
  replies: RequestItem[];
};
export function AssistanceControls({
  settings,
  act,
}: {
  settings: AssistanceSettings;
  act: (task: () => Promise<unknown>, notice?: string) => Promise<boolean>;
}) {
  const [busy, setBusy] = useState(false);
  async function toggle(
    key: "automatic_answers" | "overseer",
    enabled: boolean,
  ) {
    setBusy(true);
    try {
      await act(
        () => api("/assistance/settings", { [key]: enabled }),
        enabled ? "Assistance enabled" : "Assistance disabled",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="settings-section">
      <h2>Optional assistance</h2>
      <p>
        Both features are off by default. Your configured provider processes
        only the permitted context needed for each request.
      </p>
      <label className="check-row">
        <input
          type="checkbox"
          checked={!!settings.automatic_answers}
          disabled={
            busy ||
            (!settings.matcher_configured && !settings.automatic_answers)
          }
          onChange={(e) => void toggle("automatic_answers", e.target.checked)}
        />
        Automatic answer reuse
      </label>
      <p className="small muted">
        Match differently worded questions to published answers within the same
        resource / policy / version key and allowed audience. Each result
        records its source. Approvals are never reused.
      </p>
      {!settings.matcher_configured && (
        <p className="small muted">
          A matching provider must be configured before enabling this.
        </p>
      )}
      <label className="check-row">
        <input
          type="checkbox"
          checked={!!settings.overseer}
          disabled={
            busy || (!settings.overseer_configured && !settings.overseer)
          }
          onChange={(e) => void toggle("overseer", e.target.checked)}
        />
        Overseer assistant
      </label>
      <p className="small muted">
        Ask about permitted agents from the Agents page. The overseer inherits
        the asker's current permissions. You review recipients and confirm
        before it sends a message, question or task.
      </p>
      {!settings.overseer_configured && (
        <p className="small muted">
          An overseer provider must be configured before enabling this.
        </p>
      )}
    </section>
  );
}

export function OverseerPanel({
  groups,
  visibleAgentIDs,
}: {
  groups: Group[];
  visibleAgentIDs: string[];
}) {
  const [prompt, setPrompt] = useState(""),
    [group, setGroup] = useState("");
  const [plan, setPlan] = useState<Plan | null>(null),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  const [recent, setRecent] = useState<{ id: string; prompt: string }[]>([]);
  const [summaryError, setSummaryError] = useState("");
  const attemptedSummary = useRef("");
  const generation = useRef(0),
    abort = useRef<AbortController | null>(null);
  const visible = new Set(visibleAgentIDs);
  const readable =
    !plan ||
    (plan.context_agent_ids ?? plan.sources.map((s) => s.agent.id)).every(
      (id) => visible.has(id),
    );
  useEffect(() => {
    const controller = new AbortController();
    void api<Plan[]>("/fleet/plans", undefined, { signal: controller.signal })
      .then((plans) => {
        if (!controller.signal.aborted)
          setRecent(plans.map(({ id, prompt }) => ({ id, prompt })));
      })
      .catch(() => {});
    return () => controller.abort();
  }, []);
  async function load(id: string) {
    if (!id) return;
    const current = ++generation.current;
    abort.current?.abort();
    const controller = new AbortController();
    abort.current = controller;
    setBusy(true);
    setError("");
    setSummaryError("");
    setPlan(null);
    try {
      const result = await api<Plan>("/fleet/plans/" + id, undefined, {
        signal: controller.signal,
      });
      if (current === generation.current) setPlan(result);
    } catch (e) {
      if (current === generation.current && !controller.signal.aborted)
        setError((e as Error).message);
    } finally {
      if (current === generation.current) setBusy(false);
    }
  }
  useEffect(
    () => () => {
      ++generation.current;
      abort.current?.abort();
    },
    [],
  );
  useEffect(() => {
    if (!readable) {
      ++generation.current;
      abort.current?.abort();
      setPlan(null);
      setBusy(false);
      setError("Access changed. Ask again using your current permissions.");
    }
  }, [readable]);
  async function run(confirm = false) {
    const current = ++generation.current;
    abort.current?.abort();
    const controller = new AbortController();
    abort.current = controller;
    setBusy(true);
    setError("");
    setSummaryError("");
    if (!confirm) setPlan(null);
    try {
      const result = await api<Plan>(
        confirm ? "/fleet/command" : "/fleet/plan",
        confirm
          ? { plan_id: plan!.id }
          : {
              prompt,
              ...(group ? { target: { kind: "group", id: group } } : {}),
            },
        { signal: controller.signal },
      );
      if (current === generation.current) {
        setPlan(result);
        setRecent((previous) =>
          [
            { id: result.id, prompt: result.prompt },
            ...previous.filter((p) => p.id !== result.id),
          ].slice(0, 20),
        );
      }
    } catch (e) {
      if (current === generation.current && !controller.signal.aborted) {
        setPlan(null);
        setError((e as Error).message);
      }
    } finally {
      if (current === generation.current) setBusy(false);
    }
  }
  const waiting =
    !!plan?.dispatches.some((d) => d.request_id) &&
    (!plan.replies.length || plan.replies.some((r) => r.state === "open"));
  useEffect(() => {
    if (!plan || !waiting || !readable) return;
    const id = plan.id,
      current = generation.current,
      controller = new AbortController();
    let pending = false;
    const refresh = async () => {
      if (pending) return;
      pending = true;
      try {
        const result = await api<Plan>("/fleet/plans/" + id, undefined, {
          signal: controller.signal,
        });
        if (current === generation.current && !controller.signal.aborted)
          setPlan(result);
      } catch (e) {
        if (current === generation.current && !controller.signal.aborted) {
          setPlan(null);
          setError((e as Error).message);
        }
      } finally {
        pending = false;
      }
    };
    void refresh();
    const timer = setInterval(() => void refresh(), 3000);
    return () => {
      controller.abort();
      clearInterval(timer);
    };
  }, [plan?.id, waiting, readable]);
  const settled =
    !!plan?.replies.length && plan.replies.every((r) => r.state !== "open");
  async function summarize() {
    if (!plan) return;
    attemptedSummary.current = plan.id;
    const current = generation.current,
      controller = new AbortController();
    abort.current?.abort();
    abort.current = controller;
    setSummaryError("");
    setBusy(true);
    try {
      const result = await api<Plan>(
        "/fleet/plans/" + plan.id + "/summary",
        {},
        { signal: controller.signal },
      );
      if (current === generation.current && !controller.signal.aborted)
        setPlan(result);
    } catch (e) {
      if (current === generation.current && !controller.signal.aborted)
        setSummaryError((e as Error).message);
    } finally {
      if (current === generation.current) setBusy(false);
    }
  }
  useEffect(() => {
    if (
      !plan ||
      !settled ||
      plan.reply_summary ||
      !readable ||
      attemptedSummary.current === plan.id
    )
      return;
    const timer = setTimeout(() => {
      if (attemptedSummary.current === plan.id) return;
      attemptedSummary.current = plan.id;
      void summarize();
    }, 1100);
    return () => clearTimeout(timer);
  }, [plan?.id, settled, !!plan?.reply_summary, readable]);
  return (
    <section className="settings-section overseer-panel">
      <h2>Overseer</h2>
      <p>
        Ask about your fleet or prepare instructions. It can only read and
        contact agents you are permitted to access.
      </p>
      {!!recent.length && (
        <label className="field">
          <span>Recent overseer requests</span>
          <select
            aria-label="Recent overseer requests"
            value={plan?.id ?? ""}
            disabled={busy}
            onChange={(e) => void load(e.target.value)}
          >
            <option value="">Choose a saved request</option>
            {recent.map((p) => (
              <option key={p.id} value={p.id}>
                {p.prompt}
              </option>
            ))}
          </select>
        </label>
      )}
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void run();
        }}
      >
        <label className="field">
          <span>Agents</span>
          <select value={group} onChange={(e) => setGroup(e.target.value)}>
            <option value="">All agents I may access</option>
            {groups.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </select>
        </label>
        <label className="field">
          <span>Ask the overseer</span>
          <textarea
            required
            maxLength={4000}
            rows={3}
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder="What is the research team working on?"
          />
        </label>
        <button className="primary" disabled={busy}>
          {busy ? "Working…" : "Ask overseer"}
        </button>
      </form>
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
      {plan && readable && (
        <div className="fleet-result" aria-live="polite">
          <strong>{plan.summary}</strong>
          <p className="small muted">
            Assistant interpretation · reports observed{" "}
            {new Date(plan.created_at).toLocaleString()}. Progress is
            agent-reported.
          </p>
          <h3>
            {plan.requires_confirmation
              ? "Review recipients"
              : "Sources and recipients"}
          </h3>
          {plan.sources.map((source) => (
            <div key={source.agent.id}>
              <strong>{source.agent.name}</strong>
              <span className="badge">
                {source.fresh ? "Recent connection" : "Stale or offline"}
              </span>
              {source.instances.map((instance, i) => (
                <p className="small" key={i}>
                  {JSON.stringify(instance.progress)} ·{" "}
                  {new Date(instance.last_seen).toLocaleString()}
                </p>
              ))}
            </div>
          ))}
          {plan.requires_confirmation && (
            <>
              <h3>
                {plan.operation === "message"
                  ? "Message"
                  : plan.operation === "question"
                    ? "Question"
                    : "Task"}
              </h3>
              <p className="overseer-message">{plan.body}</p>
              <p className="small muted">
                Nothing has been sent. Confirmation uses your current
                permissions for every recipient.
              </p>
              <button
                className="primary"
                disabled={busy}
                onClick={() => void run(true)}
              >
                Confirm and send to {plan.sources.length}{" "}
                {plan.sources.length === 1 ? "agent" : "agents"}
              </button>
              <button
                className="secondary"
                disabled={busy}
                onClick={() => setPlan(null)}
              >
                Discard preview
              </button>
            </>
          )}
          {!!plan.dispatches.length && (
            <p role="status">
              Sent to {plan.dispatches.length} agents. Delivery is separate from
              task completion.
            </p>
          )}
          {plan.reply_summary && (
            <div>
              <h3>Reply summary</h3>
              <p>{plan.reply_summary}</p>
              <p className="small muted">
                Assistant interpretation of the individual replies below.
              </p>
            </div>
          )}
          {summaryError && (
            <p role="alert">
              {summaryError} Individual replies remain available.
            </p>
          )}
          {settled && !plan.reply_summary && (
            <button
              className="secondary"
              disabled={busy}
              onClick={() => void summarize()}
            >
              Summarize replies
            </button>
          )}
          {plan.replies.map((reply) => (
            <div key={reply.id}>
              <strong>
                {plan.sources.find((s) => s.agent.id === reply.resolved_by)
                  ?.agent.name ?? reply.title}
              </strong>
              <p>
                {reply.response
                  ? typeof reply.response.answer === "string"
                    ? reply.response.answer
                    : JSON.stringify(reply.response.answer)
                  : "Waiting for an agent response"}
              </p>
              <span className="badge">{reply.state}</span>{" "}
              <a href={"/requests/" + reply.id}>Open request</a>
            </div>
          ))}
          {plan.dispatches.map((d) => (
            <p key={d.agent_id}>
              <a href={"/conversations/" + d.conversation_id}>
                Open conversation with{" "}
                {plan.sources.find((s) => s.agent.id === d.agent_id)?.agent
                  .name ?? "agent"}
              </a>
            </p>
          ))}
        </div>
      )}
    </section>
  );
}
