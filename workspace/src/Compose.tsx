import { useEffect, useState } from "react";
import { api, bytes } from "./client";
import { useDraft, useOperation } from "./data";
import type { Action, Preview, Target } from "./contracts";
import { TargetPicker } from "./TargetPicker";
import {
  Button,
  DateValue,
  Details,
  ErrorView,
  Field,
  Form,
  JsonField,
  Modal,
} from "./ui";
import { navigate } from "./navigation";

type Draft = {
  mode: string;
  targets: Target[];
  title: string;
  body: string;
  choices: string;
  expiry: string;
  sharedKey: string;
  conversation: string;
  action: Action;
};
const action: Action = {
  name: "",
  executor_id: "",
  resource: "",
  resource_version: "",
  arguments: {},
  operation_key: "",
};
export function Compose({
  onClose,
  initial = [],
  mode = "message",
  multiple = false,
  conversation = "",
}: {
  onClose: () => void;
  initial?: Target[];
  mode?: string;
  multiple?: boolean;
  conversation?: string;
}) {
  const [saved, setSaved] = useDraft(
    "compose:" +
      mode +
      ":" +
      initial.map((t) => t.kind + t.id).join(",") +
      ":" +
      conversation,
  );
  const [draft, setDraft] = useState<Draft>(() => {
    try {
      if (saved) return JSON.parse(saved);
    } catch {}
    return {
      mode,
      targets: initial,
      title: "",
      body: "",
      choices: "",
      expiry: "",
      sharedKey: "",
      conversation,
      action,
    };
  });
  const [args, setArgs] = useState(() =>
      JSON.stringify(draft.action.arguments, null, 2),
    ),
    [preview, setPreview] = useState<Preview | null>(null),
    [error, setError] = useState<Error | null>(null),
    [previewing, setPreviewing] = useState(false);
  const operation = useOperation();
  useEffect(() => setSaved(JSON.stringify(draft)), [draft, setSaved]);
  const update = <K extends keyof Draft>(key: K, value: Draft[K]) => {
    setDraft((x) => ({ ...x, [key]: value }));
    setPreview(null);
  };
  function validate() {
    if (!draft.targets.length) throw Error("Choose a recipient.");
    if (!draft.body.trim() || bytes(draft.body) > 32768)
      throw Error("Enter a message of at most 32,768 UTF-8 bytes.");
    if (
      draft.mode !== "message" &&
      (!draft.title.trim() || bytes(draft.title) > 200)
    )
      throw Error("Enter a title of at most 200 UTF-8 bytes.");
    if (
      draft.expiry &&
      (Date.parse(draft.expiry) <= Date.now() ||
        Date.parse(draft.expiry) > Date.now() + 90 * 86400000)
    )
      throw Error("The deadline must be in the future, within 90 days.");
    if (draft.choices.split("\n").filter(Boolean).length > 20)
      throw Error("At most 20 choices are supported.");
    if (draft.mode === "approval") {
      for (const key of [
        "name",
        "executor_id",
        "resource",
        "resource_version",
        "operation_key",
      ] as const)
        if (!draft.action[key].trim())
          throw Error(
            "Complete every action field before requesting approval.",
          );
      const parsed = JSON.parse(args);
      if (!parsed || Array.isArray(parsed) || typeof parsed !== "object")
        throw Error("Arguments must be a JSON object.");
      return { ...draft.action, arguments: parsed } as Action;
    }
  }
  async function prepare() {
    setError(null);
    try {
      const descriptor = validate();
      setPreviewing(true);
      setPreview(
        await api<Preview>("/dispatch-preview", {
          targets: draft.targets,
          operation: draft.mode === "message" ? "message" : "ask",
          descriptor,
        }),
      );
    } catch (e) {
      setError(e as Error);
    } finally {
      setPreviewing(false);
    }
  }
  async function send() {
    setError(null);
    try {
      const descriptor = validate();
      if (!preview) throw Error("Review the permitted recipients first.");
      const request = {
        kind: draft.mode,
        target: draft.targets[0],
        title: draft.title,
        body: draft.body,
        choices:
          draft.mode === "question"
            ? draft.choices.split("\n").filter(Boolean)
            : [],
        expires_at: draft.expiry
          ? new Date(draft.expiry).toISOString()
          : undefined,
        shared_key: draft.sharedKey,
        conversation_id: draft.conversation,
        action: descriptor,
        preview_token: preview.token,
      };
      const body =
        draft.mode === "message"
          ? {
              target: draft.targets[0],
              body: draft.body,
              title: draft.title,
              conversation_id: draft.conversation,
              preview_token: preview.token,
            }
          : request;
      const batch = draft.targets.length > 1;
      const path = batch
        ? draft.mode === "message"
          ? "/message-batches"
          : "/request-batches"
        : draft.mode === "message"
          ? "/messages"
          : "/requests";
      const result = await operation.run<{
        conversation_id?: string;
        id?: string;
      }>(
        path,
        batch
          ? draft.mode === "message"
            ? {
                targets: draft.targets,
                body: draft.body,
                title: draft.title,
                preview_token: preview.token,
              }
            : { targets: draft.targets, request, preview_token: preview.token }
          : body,
      );
      if (result) {
        setSaved("");
        onClose();
        if (result.conversation_id)
          navigate("/conversations/" + result.conversation_id);
        else if (result.id) navigate("/requests/" + result.id);
      }
    } catch (e) {
      setError(e as Error);
    }
  }
  return (
    <Modal
      title={
        draft.mode === "message"
          ? "New conversation"
          : draft.mode === "question"
            ? "Ask a question"
            : draft.mode === "approval"
              ? "Request approval"
              : "Request a task"
      }
      onClose={onClose}
      wide
    >
      <Form onSubmit={() => void (preview ? send() : prepare())}>
        <Field label="Operation">
          <select
            value={draft.mode}
            onChange={(e) => update("mode", e.target.value)}
          >
            <option value="message">Message</option>
            <option value="question">Question</option>
            <option value="task">Task</option>
            <option value="approval">Approval</option>
          </select>
        </Field>
        <TargetPicker
          value={draft.targets}
          onChange={(v) => update("targets", v)}
          multiple={multiple}
        />
        <Field
          label={
            draft.mode === "message" ? "Conversation title (optional)" : "Title"
          }
        >
          <input
            required={draft.mode !== "message"}
            maxLength={200}
            value={draft.title}
            onChange={(e) => update("title", e.target.value)}
          />
        </Field>
        <Field label={draft.mode === "message" ? "Message" : "Context"}>
          <textarea
            rows={5}
            required
            value={draft.body}
            onChange={(e) => update("body", e.target.value)}
          />
        </Field>
        {draft.mode === "question" ? (
          <Field
            label="Choices (optional)"
            hint="One exact choice per line, up to 20. Leave empty for a written answer."
          >
            <textarea
              rows={3}
              value={draft.choices}
              onChange={(e) => update("choices", e.target.value)}
            />
          </Field>
        ) : null}
        {draft.mode !== "message" ? (
          <>
            <div className="form-grid">
              <Field label="Deadline (optional)">
                <input
                  type="datetime-local"
                  value={draft.expiry}
                  onChange={(e) => update("expiry", e.target.value)}
                />
              </Field>
              <Field label="Existing conversation (optional)">
                <input
                  value={draft.conversation}
                  onChange={(e) => update("conversation", e.target.value)}
                />
              </Field>
            </div>
            {draft.mode === "question" ? (
              <Field
                label="Shared informational key (optional)"
                hint="Matching content may be grouped. This does not grant permission or publish an answer."
              >
                <input
                  maxLength={200}
                  value={draft.sharedKey}
                  onChange={(e) => update("sharedKey", e.target.value)}
                />
              </Field>
            ) : null}
          </>
        ) : null}
        {draft.mode === "approval" ? (
          <section className="approval">
            <h3>Exact proposed action</h3>
            <p className="help">
              The runtime must redeem the approved action before performing the
              protected effect. Approval alone does not execute it.
            </p>
            <div className="form">
              {(
                [
                  "name",
                  "executor_id",
                  "resource",
                  "resource_version",
                  "operation_key",
                ] as const
              ).map((key) => (
                <Field
                  label={
                    {
                      name: "Action name",
                      executor_id: "Executor identity ID",
                      resource: "Resource ID",
                      resource_version: "Resource version",
                      operation_key: "Operation key",
                    }[key]
                  }
                  key={key}
                >
                  <input
                    required
                    value={draft.action[key]}
                    onChange={(e) =>
                      update("action", {
                        ...draft.action,
                        [key]: e.target.value,
                      })
                    }
                  />
                </Field>
              ))}
              <JsonField
                label="Exact arguments (JSON)"
                value={args}
                onChange={(v) => {
                  setArgs(v);
                  setPreview(null);
                }}
              />
            </div>
          </section>
        ) : null}
        <ErrorView error={error || operation.error} />
        {preview ? (
          <section className="approval">
            <h3>Review recipients</h3>
            <p className="help">
              {draft.targets[0]?.kind === "group"
                ? "Group recipients have private conversation projections. A group request uses first-authorized-response-wins."
                : "Independent requests retain their own outcomes. Logical agents receive through one eligible instance."}
            </p>
            {preview.recipients.map((r) => (
              <div className="section" key={r.target.kind + r.target.id}>
                <strong>{r.name}</strong>
                <small>
                  {" "}
                  · {r.target.kind} · {r.target.id}
                </small>
                <Details value={r.routes} label="Permitted routes" />
              </div>
            ))}
            {preview.hidden_recipients ? (
              <small>
                {preview.hidden_recipients} additional recipients with protected
                identity details.
              </small>
            ) : null}
            <small>
              Preview expires <DateValue value={preview.expires_at} />. Current
              policy is checked again when sent.
            </small>
          </section>
        ) : null}
        <div className="form-actions">
          <Button onClick={onClose} type="button">
            Keep draft and close
          </Button>
          {preview ? (
            <Button type="button" onClick={() => setPreview(null)}>
              Edit
            </Button>
          ) : null}
          <Button
            primary
            type="submit"
            disabled={operation.pending || previewing}
          >
            {operation.pending
              ? "Sending"
              : previewing
                ? "Checking recipients"
                : preview
                  ? "Confirm and send"
                  : "Review recipients"}
          </Button>
        </div>
      </Form>
    </Modal>
  );
}
