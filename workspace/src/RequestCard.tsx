import { FileLinks } from "./Attachments";
import { MentionEditor, MentionText } from "./Mentions";
import { useState } from "react";
import { useCapabilities, useOperation, useSession } from "./data";
import type { Request, Identity, Target, Mention } from "./contracts";
import { TargetPicker } from "./TargetPicker";
import {
  Badge,
  Button,
  Details,
  ErrorView,
  Field,
  Form,
  IdentityIcon,
  Modal,
} from "./ui";

export function RequestCard({
  request: r,
  sender,
  participants = [],
  onFollowUp,
}: {
  request: Request;
  sender?: Identity;
  participants?: Identity[];
  onFollowUp?: () => void;
}) {
  const caps = useCapabilities(undefined, r.id),
    op = useOperation("response:" + r.id),
    me = useSession();
  const [answer, setAnswer] = useState(""),
    [confirm, setConfirm] = useState(""),
    [sharing, setSharing] = useState(false),
    [replacing, setReplacing] = useState(false);
  async function respond(outcome: string) {
    const result = await op.run("/requests/" + r.id + "/responses", {
      version: r.version,
      outcome,
      ...(r.kind === "question" ? { answer } : {}),
    });
    if (result) {
      setAnswer("");
      setConfirm("");
    }
  }
  return (
    <article className="message request-message">
      <IdentityIcon
        kind={sender?.kind || "agent"}
        id={r.requester_id}
        label={sender?.name}
      />
      <div className="message-body">
        <div className="message-meta">
          <strong>{sender?.name || "Participant"}</strong>
          <span>
            {r.kind === "approval"
              ? "Approval"
              : r.choices?.length
                ? "Choose an answer"
                : "Message"}
          </span>
        </div>
        <div className="decision-card">
          <p>
            <MentionText
              body={r.body || r.title}
              mentions={r.mentions}
              participants={participants}
            />
          </p>
          <FileLinks files={r.attachments} />
          {r.body &&
          r.title &&
          r.title !== r.body &&
          !r.body.startsWith(r.title) ? (
            <small>{r.title}</small>
          ) : null}
          <ErrorView error={op.error} />
          {r.state !== "open" ? (
            <Badge>
              {r.response?.outcome === "denied"
                ? "Declined"
                : r.response?.outcome === "approved"
                  ? "Approved"
                  : r.response?.outcome === "answered"
                    ? "Answered"
                    : r.state === "superseded"
                      ? "Replaced"
                      : r.state}
            </Badge>
          ) : caps.data?.operations.respond ? (
            <>
              {r.kind === "task" ? (
                <div className="decision-actions">
                  {[
                    ["completed", "Mark completed"],
                    ["failed", "Report failed"],
                    ["rejected", "Reject task"],
                  ].map(([outcome, label]) => (
                    <Button
                      key={outcome}
                      disabled={op.pending}
                      onClick={() => void respond(outcome)}
                    >
                      {label}
                    </Button>
                  ))}
                </div>
              ) : r.kind === "approval" ? (
                <div className="decision-actions">
                  <Button
                    primary
                    disabled={op.pending}
                    onClick={() =>
                      r.action
                        ? setConfirm("approved")
                        : void respond("approved")
                    }
                  >
                    Approve
                  </Button>
                  <Button
                    disabled={op.pending}
                    onClick={() =>
                      r.action ? setConfirm("denied") : void respond("denied")
                    }
                  >
                    Deny
                  </Button>
                  {onFollowUp ? (
                    <Button onClick={onFollowUp}>Ask for context</Button>
                  ) : null}
                </div>
              ) : (
                <Form onSubmit={() => void respond("answered")}>
                  {r.choices?.length ? (
                    <fieldset className="reply-choices">
                      <legend>Choose an answer</legend>
                      {r.choices.map((choice) => (
                        <label key={choice}>
                          <input
                            type="radio"
                            name={r.id}
                            value={choice}
                            checked={answer === choice}
                            onChange={() => setAnswer(choice)}
                          />
                          {choice}
                        </label>
                      ))}
                    </fieldset>
                  ) : (
                    <Field label="Reply">
                      <textarea
                        value={answer}
                        onChange={(e) => setAnswer(e.target.value)}
                        rows={2}
                      />
                    </Field>
                  )}
                  <Button
                    primary
                    disabled={op.pending || !answer.trim()}
                    type="submit"
                  >
                    Send reply
                  </Button>
                </Form>
              )}
            </>
          ) : (
            <small>
              {caps.isPending ? "Checking access..." : "Waiting for a reply"}
            </small>
          )}
          {confirm ? (
            <div className="bound-confirm">
              <p>
                {confirm === "cancel"
                  ? "Stop waiting for a response? The message and its history will remain."
                  : "Record this decision for the exact action below?"}
              </p>
              {confirm !== "cancel" ? (
                <Details label="Action details" value={r.action} />
              ) : null}
              <div className="actions">
                <Button onClick={() => setConfirm("")}>Back</Button>
                <Button
                  primary
                  disabled={op.pending}
                  onClick={() =>
                    confirm === "cancel"
                      ? void op
                          .run(`/requests/${r.id}/cancel`, {
                            version: r.version,
                          })
                          .then((result) => {
                            if (result) setConfirm("");
                          })
                      : void respond(confirm)
                  }
                >
                  {confirm === "cancel"
                    ? "Cancel pending request"
                    : `Confirm ${confirm === "approved" ? "approval" : "decline"}`}
                </Button>
              </div>
            </div>
          ) : null}
          {r.response?.source_answer_id ? (
            <div className="answer-provenance">
              <Badge tone={r.response.source_invalidated ? "warn" : "neutral"}>
                {r.response.source_invalidated
                  ? "Saved answer invalidated"
                  : "Reused saved answer"}
              </Badge>
              <details>
                <summary>Answer source</summary>
                <p>
                  {r.response.reuse_reason ||
                    "Matched the same question, scope and permitted audience."}
                </p>
                <small>{r.response.source_answer_id}</small>
              </details>
            </div>
          ) : null}
          <div className="actions request-tools">
            {caps.data?.operations.replace ? (
              <Button disabled={op.pending} onClick={() => setReplacing(true)}>
                Edit and resend
              </Button>
            ) : null}
            {r.kind === "question" &&
            r.shared_key &&
            me.can_publish_answers &&
            ((r.state === "open" && caps.data?.operations.respond) ||
              (r.state === "resolved" &&
                r.response?.outcome === "answered")) ? (
              <Button onClick={() => setSharing(true)}>
                {r.state === "open"
                  ? "Answer and share"
                  : "Save reusable answer"}
              </Button>
            ) : null}
            {caps.data?.operations.cancel ? (
              <Button
                disabled={op.pending}
                onClick={() => setConfirm("cancel")}
              >
                {r.kind === "approval"
                  ? "Cancel approval"
                  : "Cancel pending reply"}
              </Button>
            ) : null}
          </div>
        </div>
      </div>
      {sharing ? (
        <ShareAnswer
          request={r}
          sender={sender}
          answer={answer}
          onClose={() => setSharing(false)}
        />
      ) : null}
      {replacing ? (
        <ReplaceMessage
          request={r}
          participants={participants}
          onClose={() => setReplacing(false)}
        />
      ) : null}
    </article>
  );
}

function ReplaceMessage({
  request: r,
  participants,
  onClose,
}: {
  request: Request;
  participants: Identity[];
  onClose: () => void;
}) {
  const op = useOperation("replace:" + r.id);
  const previous = op.previous<{ body: string; mentions?: Mention[] }>(
    `/requests/${r.id}/replace`,
  );
  const [body, setBody] = useState(previous?.body || r.body || r.title),
    [mentions, setMentions] = useState<Mention[]>(
      previous?.mentions || r.mentions || [],
    );
  return (
    <Modal title="Replace pending message" onClose={onClose}>
      <Form
        onSubmit={async () => {
          const result = await op.run(`/requests/${r.id}/replace`, {
            version: r.version,
            body,
            mentions,
          });
          if (result) onClose();
        }}
      >
        <p className="help">
          Send an updated message to the same recipients. The original stays in
          history.
        </p>
        <MentionEditor
          value={body}
          onChange={setBody}
          mentions={mentions}
          onMentions={setMentions}
          participants={participants}
          onKeyDown={() => {}}
        />
        {r.choices?.length ? (
          <div className="actions">
            {r.choices.map((choice) => (
              <Badge key={choice}>{choice}</Badge>
            ))}
          </div>
        ) : null}
        <FileLinks files={r.attachments} />
        {r.kind === "approval" ? (
          <small className="muted">
            A fresh approval is required for the updated message.
          </small>
        ) : null}
        {r.action ? (
          <Details label="Unchanged action" value={r.action} />
        ) : null}
        <ErrorView error={op.error} />
        <div className="form-actions">
          <Button onClick={onClose}>Back</Button>
          <Button
            primary
            type="submit"
            disabled={
              op.pending || !body.trim() || body === (r.body || r.title)
            }
          >
            Send replacement
          </Button>
        </div>
      </Form>
    </Modal>
  );
}

function ShareAnswer({
  request: r,
  sender,
  answer: draft,
  onClose,
}: {
  request: Request;
  sender?: Identity;
  answer: string;
  onClose: () => void;
}) {
  const pending = r.state === "open";
  const [answer, setAnswer] = useState(draft),
    [audience, setAudience] = useState<Target[]>([
      {
        kind:
          sender?.kind ||
          (r.requester_id.startsWith("agt_")
            ? "agent"
            : r.requester_id.startsWith("svc_")
              ? "service"
              : "person"),
        id: r.requester_id,
      },
    ]);
  const op = useOperation("share:" + r.id);
  async function save() {
    const result = await op.run(
      pending ? `/requests/${r.id}/answer-and-share` : "/answers",
      pending
        ? {
            version: r.version,
            outcome: "answered",
            answer,
            audience: audience.map((a) => a.id),
          }
        : { request_id: r.id, audience: audience.map((a) => a.id) },
    );
    if (result) onClose();
  }
  return (
    <Modal
      title={pending ? "Answer and share" : "Save reusable answer"}
      onClose={onClose}
    >
      <div className="form">
        <p>{r.body || r.title}</p>
        {pending ? (
          r.choices?.length ? (
            <Field label="Answer">
              <select
                value={answer}
                onChange={(e) => setAnswer(e.target.value)}
              >
                <option value="">Choose an answer</option>
                {r.choices.map((c) => (
                  <option key={c} value={c}>
                    {c}
                  </option>
                ))}
              </select>
            </Field>
          ) : (
            <Field label="Answer">
              <textarea
                rows={3}
                value={answer}
                onChange={(e) => setAnswer(e.target.value)}
              />
            </Field>
          )
        ) : (
          <p className="prose">{String(r.response?.answer ?? "")}</p>
        )}
        <div
          className="field"
          role="group"
          aria-label="Who can reuse this answer?"
        >
          <span>Who can reuse this answer?</span>
          <TargetPicker
            value={audience}
            onChange={setAudience}
            multiple
            allowGroups={false}
            allowInstances={false}
          />
        </div>
        <p className="help">
          Available for 24 hours to these contacts, for this exact informational
          question. Contact permissions still apply.
        </p>
        <ErrorView error={op.error} />
        <div className="form-actions">
          <Button onClick={onClose}>Cancel</Button>
          <Button
            primary
            disabled={
              op.pending || !audience.length || (pending && !answer.trim())
            }
            onClick={() => void save()}
          >
            {pending ? "Send answer and share" : "Save answer"}
          </Button>
        </div>
      </div>
    </Modal>
  );
}
