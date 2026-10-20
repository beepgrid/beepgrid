import { useEffect, useState } from "react";
import { api, type Entity, type RequestItem, type Target } from "./api";
type SharedAnswer = {
  id: string;
  key: string;
  question: string;
  answer: unknown;
  audience: string[];
  valid_until: string;
  author_id: string;
  source_request: string;
};
type Proposal = { id: string; reason: string; answer: SharedAnswer };
export function AnswerSuggestions({
  request,
  onChanged,
}: {
  request: RequestItem;
  onChanged: () => void;
}) {
  const [proposals, setProposals] = useState<Proposal[]>([]),
    [status, setStatus] = useState(""),
    [busy, setBusy] = useState(false);
  useEffect(() => {
    setProposals([]);
    setStatus("");
  }, [request.id]);
  async function find() {
    setBusy(true);
    try {
      const r = await api<{
        proposals: Proposal[];
        matcher_configured: boolean;
      }>("/requests/" + request.id + "/suggestions", {});
      setProposals(r.proposals);
      setStatus(
        r.matcher_configured
          ? r.proposals.length
            ? "Review scope before applying an answer."
            : "No compatible answer was suggested."
          : "A semantic judge is not configured. Exact scoped reuse still works.",
      );
    } catch (e) {
      setStatus((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  async function decide(p: Proposal, accept: boolean) {
    setBusy(true);
    try {
      await api("/answer-proposals/" + p.id, { accept });
      setProposals(proposals.filter((x) => x.id !== p.id));
      if (accept) onChanged();
    } catch (e) {
      setStatus((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="answer-suggestions">
      <button className="quiet" disabled={busy} onClick={() => void find()}>
        Find a related answer
      </button>
      {status && (
        <p className="small muted" role="status">
          {status}
        </p>
      )}
      {proposals.map((p) => (
        <div className="suggestion" key={p.id}>
          <strong>{p.answer.question}</strong>
          <p>
            {typeof p.answer.answer === "string"
              ? p.answer.answer
              : JSON.stringify(p.answer.answer)}
          </p>
          <p className="small muted">{p.reason}</p>
          <p className="small">
            <code>{p.answer.key}</code> · valid until{" "}
            {new Date(p.answer.valid_until).toLocaleString()}
          </p>
          <div>
            <button
              className="secondary"
              disabled={busy}
              onClick={() => void decide(p, false)}
            >
              Keep separate
            </button>
            <button
              className="primary"
              disabled={busy}
              onClick={() => void decide(p, true)}
            >
              Scope matches, use answer
            </button>
          </div>
        </div>
      ))}
    </div>
  );
}
export function PublishAnswerForm({
  request,
  people,
  onDone,
}: {
  request: RequestItem;
  people: Entity[];
  onDone: () => void;
}) {
  const [key, setKey] = useState(request.shared_key ?? ""),
    [audience, setAudience] = useState<string[]>([request.requester_id]),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  return (
    <form
      className="modal-body"
      onSubmit={async (e) => {
        e.preventDefault();
        setBusy(true);
        try {
          await api("/answers", {
            request_id: request.id,
            key,
            audience,
            valid_until: new Date(Date.now() + 24 * 3600000).toISOString(),
          });
          onDone();
        } catch (e) {
          setError((e as Error).message);
        } finally {
          setBusy(false);
        }
      }}
    >
      <p>
        Share this informational answer only with the selected audience.
        Compatible pending questions will receive it; approvals remain separate.
      </p>
      <label className="field">
        <span>Resource / policy / version key</span>
        <input
          required
          value={key}
          onChange={(e) => setKey(e.target.value)}
          placeholder="report/india/fy2026/v1"
        />
      </label>
      <div className="member-picker">
        <strong>May reuse this answer</strong>
        {people.map((p) => (
          <label className="check-row" key={p.id}>
            <input
              type="checkbox"
              checked={audience.includes(p.id)}
              onChange={(e) =>
                setAudience(
                  e.target.checked
                    ? [...audience, p.id]
                    : audience.filter((x) => x !== p.id),
                )
              }
            />
            {p.name} · {p.kind}
          </label>
        ))}
      </div>
      <p className="small muted">
        This publication expires in 24 hours. You can invalidate it earlier in
        Shared answers.
      </p>
      {error && (
        <p role="alert" className="error">
          {error}
        </p>
      )}
      <button className="primary wide" disabled={busy || !audience.length}>
        Publish scoped answer
      </button>
    </form>
  );
}
export function SharedAnswers() {
  const [answers, setAnswers] = useState<SharedAnswer[]>([]),
    [error, setError] = useState("");
  const load = () =>
    api<SharedAnswer[]>("/answers")
      .then(setAnswers)
      .catch((e) => setError(e.message));
  useEffect(() => {
    void load();
  }, []);
  return (
    <section className="settings-section">
      <h2>Shared answers</h2>
      <p>
        Reusable information with an explicit audience, source and expiry.
        Invalidating it stops future reuse and notifies consumers.
      </p>
      {error && <p className="error">{error}</p>}
      {answers.length === 0 && (
        <p className="small muted">
          No active shared answers. Publish an answered question from the inbox.
        </p>
      )}
      {answers.map((a) => (
        <div className="answer-record" key={a.id}>
          <div>
            <strong>{a.question}</strong>
            <small>
              {a.key} · {a.audience.length} audience members · expires{" "}
              {new Date(a.valid_until).toLocaleString()}
            </small>
            <p>
              {typeof a.answer === "string"
                ? a.answer
                : JSON.stringify(a.answer)}
            </p>
          </div>
          <button
            className="secondary"
            onClick={async () => {
              try {
                await api("/answers/" + a.id + "/invalidate", {});
                await load();
              } catch (e) {
                setError((e as Error).message);
              }
            }}
          >
            Invalidate
          </button>
        </div>
      ))}
    </section>
  );
}
export function FleetSummary() {
  const [result, setResult] = useState<{
      summary: string;
      total: number;
      fresh: number;
      as_of: string;
      agents: {
        agent: Entity;
        fresh: boolean;
        instances: {
          id: string;
          progress: Record<string, unknown>;
          last_seen: string;
        }[];
      }[];
    } | null>(null),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  async function query() {
    setBusy(true);
    try {
      setResult(await api("/fleet/status", {}));
      setError("");
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="fleet-summary">
      <div>
        <span className="eyebrow">FLEET STATUS</span>
        <h3>A current view of the agents you may access.</h3>
        <p>
          Read-only. Shows sources and coverage; worker-reported counts are not
          added together.
        </p>
      </div>
      <button
        className="secondary"
        disabled={busy}
        onClick={() => void query()}
      >
        {busy ? "Collecting…" : "Ask for fleet status"}
      </button>
      {error && <p className="error">{error}</p>}
      {result && (
        <div className="fleet-result">
          <strong>{result.summary}</strong>
          <small>Observed {new Date(result.as_of).toLocaleString()}</small>
          {result.agents.map((a) => (
            <div key={a.agent.id}>
              <span>{a.agent.name}</span>
              <span className={"badge " + (a.fresh ? "green" : "muted")}>
                {a.fresh ? "Fresh connection" : "No fresh connection"}
              </span>
              <code>
                {a.instances
                  .map((i) =>
                    Object.keys(i.progress ?? {}).length
                      ? JSON.stringify(i.progress)
                      : "No progress reported",
                  )
                  .join(" · ")}
              </code>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
