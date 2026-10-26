import { useEffect, useState, useRef, type FormEvent } from "react";
import {
  Client,
  type RequestItem,
  type Response,
  type Message,
} from "@notif/client";
export type { Client, RequestItem };
// Browser integration uses the application's authenticated same-origin HttpOnly session.
// Never pass an owner, enrollment or provider credential to a browser.
export function browserClient(baseURL: string = window.location.origin) {
  return new Client(baseURL, "");
}
export function useNotifRequest(client: Client, id: string) {
  const [request, setRequest] = useState<RequestItem | null>(null),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  const generation = useRef(0);
  useEffect(() => {
    ++generation.current;
    setBusy(false);
    const abort = new AbortController();
    setRequest(null);
    setError("");
    let pending = false;
    const refresh = async () => {
      if (pending) return;
      pending = true;
      try {
        const r = await client.getRequest(id, abort.signal);
        if (!abort.signal.aborted) {
          setRequest(r);
          setError("");
        }
      } catch (e) {
        if (!abort.signal.aborted) {
          setRequest(null);
          setError((e as Error).message);
        }
      } finally {
        pending = false;
      }
    };
    void refresh();
    const timer = setInterval(() => void refresh(), 1000);
    return () => {
      ++generation.current;
      abort.abort();
      clearInterval(timer);
    };
  }, [client, id]);
  async function respond(outcome: Response["outcome"], answer?: unknown) {
    if (!request || request.id !== id) return;
    const current = generation.current;
    setBusy(true);
    setError("");
    try {
      const result = await client.respond(id, {
        outcome,
        answer,
        version: request.version,
      });
      if (current === generation.current) setRequest(result);
    } catch (e) {
      if (current === generation.current) setError((e as Error).message);
    } finally {
      if (current === generation.current) setBusy(false);
    }
  }
  return { request, error, busy, respond };
}
export function NotifRequest({
  client,
  id,
  onConversation,
  onResolved,
}: {
  client: Client;
  id: string;
  onConversation?: (id: string) => void;
  onResolved?: (request: RequestItem) => void;
}) {
  const { request: r, error, busy, respond } = useNotifRequest(client, id);
  const [answer, setAnswer] = useState("");
  const notified = useRef("");
  useEffect(() => {
    setAnswer("");
    notified.current = "";
  }, [id]);
  useEffect(() => {
    if (r && r.state !== "open" && notified.current !== r.id) {
      notified.current = r.id;
      onResolved?.(r);
    }
  }, [r, onResolved]);
  return (
    <section className="notif-request" aria-label="Agent request">
      {error && <p role="alert">{error}</p>}
      {!r || r.id !== id ? (
        <p role="status">Loading request…</p>
      ) : (
        <>
          <h3>{r.title}</h3>
          <p>{r.body}</p>
          {r.response?.source_answer_id && (
            <p role="status">
              {r.response.reuse_method === "automatic"
                ? "Answered from a published source."
                : "Answered from a shared answer."}{" "}
              {r.response.reuse_reason}
            </p>
          )}
          {r.response?.source_invalidated && (
            <p role="alert">
              The source answer is no longer valid. Review before relying on it.
            </p>
          )}
          {r.state !== "open" ? (
            <p role="status">
              {r.response?.outcome ?? r.state}
              {r.response?.answer !== undefined
                ? ": " + String(r.response.answer)
                : ""}
            </p>
          ) : (
            <>
              {onConversation && (
                <button
                  type="button"
                  onClick={() => onConversation(r.conversation_id)}
                >
                  Ask for clarification
                </button>
              )}
              {r.kind === "approval" ? (
                <div>
                  {r.action && (
                    <dl aria-label="Exact action requiring approval">
                      <dt>Action</dt>
                      <dd>{r.action.name}</dd>
                      <dt>Executor</dt>
                      <dd>{r.action.executor_id}</dd>
                      <dt>Resource</dt>
                      <dd>{r.action.resource}</dd>
                      <dt>Resource version</dt>
                      <dd>{r.action.resource_version}</dd>
                      <dt>Arguments</dt>
                      <dd>
                        <pre>{JSON.stringify(r.action.arguments, null, 2)}</pre>
                      </dd>
                      <dt>Operation</dt>
                      <dd>{r.action.operation_key}</dd>
                      <dt>Expires</dt>
                      <dd>{r.expires_at}</dd>
                    </dl>
                  )}
                  <button
                    disabled={busy}
                    onClick={() => void respond("denied")}
                  >
                    Deny
                  </button>
                  <button
                    disabled={busy}
                    onClick={() => void respond("approved")}
                  >
                    Approve
                  </button>
                </div>
              ) : r.kind === "question" ? (
                <form
                  onSubmit={(e) => {
                    e.preventDefault();
                    void respond("answered", answer);
                  }}
                >
                  <fieldset disabled={busy}>
                    <legend>Your answer</legend>
                    {r.choices?.length ? (
                      r.choices.map((c) => (
                        <label key={c}>
                          <input
                            type="radio"
                            name={"notif-answer-" + id}
                            value={c}
                            checked={answer === c}
                            onChange={() => setAnswer(c)}
                          />
                          {c}
                        </label>
                      ))
                    ) : (
                      <textarea
                        aria-label="Your answer"
                        value={answer}
                        onChange={(e) => setAnswer(e.target.value)}
                        required
                      />
                    )}
                    <button disabled={!answer.trim()}>Send answer</button>
                  </fieldset>
                </form>
              ) : (
                <div>
                  <button
                    disabled={busy}
                    onClick={() => void respond("rejected")}
                  >
                    Reject task
                  </button>
                  <button
                    disabled={busy}
                    onClick={() => void respond("completed")}
                  >
                    Report completed
                  </button>
                </div>
              )}
            </>
          )}
        </>
      )}
    </section>
  );
}
export type Conversation = { id: string; title: string; messages: Message[] };
export function NotifConversation({
  client,
  id,
}: {
  client: Client;
  id: string;
}) {
  const [c, setC] = useState<Conversation | null>(null),
    [draft, setDraft] = useState(""),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  const attempt = useRef({ body: "", key: "" });
  const generation = useRef(0);
  useEffect(() => {
    ++generation.current;
    attempt.current = { body: "", key: "" };
    setBusy(false);
    setC(null);
    setDraft("");
    setError("");
    const abort = new AbortController();
    let pending = false;
    const refresh = async () => {
      if (pending) return;
      pending = true;
      try {
        const next = await client.call<Conversation>(
          "GET",
          "/api/v1/conversations/" + encodeURIComponent(id),
          undefined,
          { signal: abort.signal },
        );
        if (!abort.signal.aborted) {
          setC(next);
          setError("");
        }
      } catch (e) {
        if (!abort.signal.aborted) {
          setC(null);
          setError((e as Error).message);
        }
      } finally {
        pending = false;
      }
    };
    void refresh();
    const timer = setInterval(() => void refresh(), 1000);
    return () => {
      ++generation.current;
      abort.abort();
      clearInterval(timer);
    };
  }, [client, id]);
  async function send(e: FormEvent) {
    e.preventDefault();
    if (!c || c.id !== id || busy) return;
    const current = generation.current;
    setBusy(true);
    if (attempt.current.body !== draft || !attempt.current.key)
      attempt.current = { body: draft, key: crypto.randomUUID() };
    try {
      await client.notify(
        { conversation_id: id, body: draft },
        attempt.current.key,
      );
      if (current === generation.current) {
        setDraft("");
        attempt.current = { body: "", key: "" };
        setError("");
      }
    } catch (e) {
      if (current === generation.current) setError((e as Error).message);
    } finally {
      if (current === generation.current) setBusy(false);
    }
  }
  return (
    <section className="notif-conversation" aria-label="Conversation">
      {error && <p role="alert">{error}</p>}
      {c && c.id === id ? (
        <>
          <h3>{c.title}</h3>
          <div role="log" aria-live="polite">
            {c.messages.map((m) => (
              <article key={m.id}>
                <small>
                  {m.sender_id}
                  {m.instance_id ? " · " + m.instance_id : ""}
                </small>
                <p>{m.body}</p>
              </article>
            ))}
          </div>
          <form onSubmit={(e) => void send(e)}>
            <textarea
              aria-label="Message"
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              maxLength={32768}
              required
            />
            <button disabled={busy || !draft.trim()}>Send</button>
          </form>
        </>
      ) : (
        <p role="status">Loading conversation…</p>
      )}
    </section>
  );
}
