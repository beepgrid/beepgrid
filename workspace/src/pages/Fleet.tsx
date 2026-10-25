import { useState } from "react";
import { useOperation, usePostResource, useResource } from "../data";
import type {
  AssistanceSettings,
  FleetSource,
  Group,
  Plan,
} from "../contracts";
import {
  Badge,
  Button,
  DateValue,
  Details,
  ErrorView,
  Field,
  Form,
  Loading,
  Status,
} from "../ui";
import { readable } from "../client";
import { navigate } from "../navigation";

export default function Fleet() {
  const [group, setGroup] = useState(""),
    [prompt, setPrompt] = useState(""),
    [plan, setPlan] = useState<Plan | null>(null),
    [notice, setNotice] = useState("");
  const groups = useResource<Group[]>("/groups"),
    settings = useResource<AssistanceSettings>("/assistance/settings"),
    status = usePostResource<{
      agents: FleetSource[];
      summary: string;
      as_of: string;
    }>("/fleet/status", group ? { target: { kind: "group", id: group } } : {}),
    plans = useResource<Plan[]>(
      "/fleet/plans",
      settings.data?.overseer === true,
    ),
    op = useOperation();
  const detail = useResource<Plan>(
    plan ? "/fleet/plans/" + plan.id : "/fleet/plans",
    !!plan && settings.data?.overseer === true,
  );
  const current = detail.data || plan;
  return (
    <div className="form">
      <Field label="Fleet scope">
        <select value={group} onChange={(e) => setGroup(e.target.value)}>
          <option value="">All permitted agents (up to 100)</option>
          {groups.data?.map((g) => (
            <option key={g.id} value={g.id}>
              {g.name}
            </option>
          ))}
        </select>
      </Field>
      <ErrorView error={status.error || settings.error || op.error} />
      {status.isPending ? <Loading label="Reading reported status" /> : null}
      {status.data ? (
        <>
          <p>{status.data.summary}</p>
          <small>
            Observed <DateValue value={status.data.as_of} />
          </small>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Agent</th>
                  <th>Report freshness</th>
                  <th>Instances</th>
                </tr>
              </thead>
              <tbody>
                {status.data.agents.map((a) => (
                  <tr key={a.agent.id}>
                    <td>{a.agent.name}</td>
                    <td>
                      <Badge tone={a.fresh ? "good" : "neutral"}>
                        {a.fresh ? "Fresh connection" : "Stale or offline"}
                      </Badge>
                    </td>
                    <td>
                      {a.instances.map((i) => (
                        <div key={i.id}>
                          <Status value={i.presence} />
                          <Details
                            label={i.id}
                            value={{
                              last_contact: i.last_seen,
                              reported_progress: i.progress,
                              declared_capabilities: i.capabilities,
                            }}
                          />
                        </div>
                      ))}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      ) : null}
      {settings.data?.overseer ? (
        <section className="section">
          <h2>Overseer</h2>
          <p>
            Uses your current permissions. Outgoing instructions require review
            and confirmation.
          </p>
          <Form
            onSubmit={() =>
              void op
                .run<Plan>("/fleet/plan", {
                  prompt,
                  ...(group ? { target: { kind: "group", id: group } } : {}),
                })
                .then((p) => {
                  if (p) {
                    setPlan(p);
                    setNotice("");
                  }
                })
            }
          >
            <Field label="Ask about your agents">
              <textarea
                rows={3}
                maxLength={4000}
                required
                value={prompt}
                onChange={(e) => setPrompt(e.target.value)}
              />
            </Field>
            <Button primary type="submit" disabled={op.pending}>
              {op.pending ? "Preparing a response" : "Ask overseer"}
            </Button>
          </Form>
          {op.pending ? (
            <Loading label="Reviewing permitted fleet information" />
          ) : null}
          <ErrorView error={plans.error || detail.error} />
          {current ? (
            <section className="approval">
              <h3>
                {current.operation === "status"
                  ? "Fleet interpretation"
                  : "Review proposed instruction"}
              </h3>
              <p>{current.summary}</p>
              {current.body ? <pre>{current.body}</pre> : null}
              <small>
                Assistant interpretation · Expires{" "}
                <DateValue value={current.expires_at} />
              </small>
              <Details label="Sources and freshness" value={current.sources} />
              {current.requires_confirmation && !current.dispatches?.length ? (
                <div className="form-actions">
                  <Button onClick={() => setPlan(null)}>Dismiss</Button>
                  <Button
                    primary
                    disabled={
                      op.pending || Date.parse(current.expires_at) <= Date.now()
                    }
                    onClick={() =>
                      void op
                        .run<Plan>("/fleet/command", { plan_id: current.id })
                        .then((p) => {
                          if (p) {
                            setPlan(p);
                            setNotice(
                              "Instruction accepted. Replies are tracked separately.",
                            );
                          }
                        })
                    }
                  >
                    Confirm exact instruction
                  </Button>
                </div>
              ) : null}
              {current.dispatches?.map((d) => (
                <div className="section" key={d.agent_id}>
                  <strong>{d.agent_id}</strong>
                  <div className="actions">
                    <Button
                      onClick={() =>
                        navigate("/conversations/" + d.conversation_id)
                      }
                    >
                      Conversation
                    </Button>
                    {d.request_id ? (
                      <Button
                        onClick={() => navigate("/requests/" + d.request_id)}
                      >
                        Request
                      </Button>
                    ) : null}
                  </div>
                </div>
              ))}
              {current.replies?.map((r) => (
                <div className="section" key={r.id}>
                  <Badge>{r.state}</Badge>
                  <p>{r.title}</p>
                  <pre>
                    {readable(
                      r.response?.answer ||
                        r.response?.outcome ||
                        "Awaiting an authorized reply",
                    )}
                  </pre>
                </div>
              ))}
              {current.dispatches?.length ? (
                <Button
                  disabled={op.pending}
                  onClick={() =>
                    void op
                      .run<Plan>("/fleet/plans/" + current.id + "/summary", {})
                      .then((p) => {
                        if (p) setPlan(p);
                      })
                  }
                >
                  Summarize settled replies
                </Button>
              ) : null}
              {current.reply_summary ? <p>{current.reply_summary}</p> : null}
            </section>
          ) : null}
          {notice ? (
            <p className="notice" role="status">
              {notice}
            </p>
          ) : null}
          <h3>Recent saved plans</h3>
          {plans.data?.map((p) => (
            <button
              className="conversation-row"
              key={p.id}
              onClick={() => setPlan(p)}
            >
              <strong>{p.prompt}</strong>
              <p>
                {p.operation} · <DateValue value={p.created_at} />
              </p>
            </button>
          ))}
        </section>
      ) : (
        <p className="help">
          Overseer assistance is{" "}
          {settings.data?.overseer_configured ? "off" : "not configured"}.
          Direct status, messages, questions and tasks remain available.
        </p>
      )}
    </div>
  );
}
