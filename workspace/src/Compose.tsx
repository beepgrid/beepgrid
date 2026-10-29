import { useState, type ReactNode } from "react";
import { useCollection, useDraft, useOperation, useSession } from "./data";
import { MentionEditor } from "./Mentions";
import type { Identity, Mention, Target } from "./contracts";
import { bytes } from "./client";
import { navigate } from "./navigation";
import { TargetPicker } from "./TargetPicker";
import { Button, ErrorView, Form, Icon, Modal } from "./ui";

export function MessageComposer({
  targets = [],
  conversation = "",
  onSent,
  tools,
  canAsk = true,
  replyRequired = false,
  delivery = "individual",
  participants = [],
}: {
  targets?: Target[];
  conversation?: string;
  onSent?: (id: string) => void;
  tools?: ReactNode;
  canAsk?: boolean;
  replyRequired?: boolean;
  delivery?: "individual" | "group";
  participants?: Identity[];
}) {
  const [body, setBody] = useDraft(
    "message:" +
      (conversation ||
        targets
          .map((t) => t.kind + t.id)
          .sort()
          .join(",")),
  );
  const [mentionDraft, setMentionDraft] = useDraft(
    "mentions:" +
      (conversation ||
        targets
          .map((t) => t.kind + t.id)
          .sort()
          .join(",")),
  );
  let mentions: Mention[] = [];
  try {
    const saved: unknown = JSON.parse(mentionDraft || "[]");
    if (Array.isArray(saved))
      mentions = saved.filter(
        (m): m is Mention =>
          !!m &&
          typeof m.principal_id === "string" &&
          Number.isInteger(m.start) &&
          Number.isInteger(m.end) &&
          m.start >= 0 &&
          m.end > m.start &&
          m.end <= Array.from(body).length,
      );
  } catch {}
  function setMentions(items: Mention[]) {
    setMentionDraft(JSON.stringify(items));
  }
  const [approval, setApproval] = useState(false),
    [choices, setChoices] = useState<string[]>([]),
    [error, setError] = useState<Error | null>(null);
  const op = useOperation(
    "message:" +
      (conversation ||
        targets
          .map((t) => t.kind + t.id)
          .sort()
          .join(",")),
  );
  async function send() {
    if (!body.trim() || op.pending) return;
    setError(null);
    if (bytes(body) > 32768) {
      setError(Error("Your message is too long. Keep it under 32 KB."));
      return;
    }
    if (!conversation && !targets.length) {
      setError(Error("Choose a recipient first."));
      return;
    }
    const clean = choices.map((c) => c.trim()).filter(Boolean);
    if (
      choices.length &&
      (clean.length < 2 || new Set(clean).size !== clean.length)
    ) {
      setError(Error("Add at least two different choices."));
      return;
    }
    const request = replyRequired || approval || clean.length > 0;
    let result: { conversation_id?: string } | undefined;
    if (!conversation && targets.length > 1 && delivery === "individual") {
      if (request) {
        const sent = await op.run<{ request: { conversation_id: string } }[]>(
          "/request-batches",
          {
            targets,
            request: {
              kind: approval ? "approval" : "question",
              body,
              choices: clean,
              reuse_direct: true,
            },
          },
        );
        if (sent)
          result = { conversation_id: sent[0]?.request.conversation_id };
      } else {
        const sent = await op.run<{ result: { conversation_id: string } }[]>(
          "/message-batches",
          { targets, body, reuse_direct: true },
        );
        if (sent) result = { conversation_id: sent[0]?.result.conversation_id };
      }
    } else if (!conversation && targets.length > 1) {
      result = await op.run("/conversations", {
        targets,
        body,
        approval,
        choices: clean,
        mentions,
      });
    } else if (request) {
      result = await op.run("/requests", {
        target: targets[0],
        conversation_id: conversation || undefined,
        kind: approval ? "approval" : "question",
        body,
        choices: clean,
        reuse_direct: !conversation,
        mentions,
      });
    } else {
      const sent = await op.run<{
        conversation_id?: string;
        messages?: { conversation_id: string }[];
      }>("/messages", {
        target: targets[0],
        conversation_id: conversation || undefined,
        body,
        reuse_direct: !conversation,
        mentions,
      });
      if (sent)
        result = {
          conversation_id:
            sent.conversation_id || sent.messages?.[0]?.conversation_id,
        };
    }
    if (result) {
      setBody("");
      setMentions([]);
      setChoices([]);
      setApproval(false);
      op.forget();
      onSent?.(result.conversation_id || conversation);
    }
  }
  return (
    <div className="message-compose">
      <Form onSubmit={() => void send()}>
        <ErrorView error={error || op.error} />
        <MentionEditor
          value={body}
          onChange={setBody}
          mentions={mentions}
          onMentions={setMentions}
          participants={participants}
          rows={conversation ? 2 : 3}
          onKeyDown={(e) => {
            if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
              e.preventDefault();
              void send();
            }
          }}
        />
        {approval ? (
          <div className="approval-preview" aria-label="Approval flow preview">
            <button
              type="button"
              className="icon-button"
              aria-label="Remove approval flow"
              onClick={() => setApproval(false)}
            >
              <Icon name="close" size={15} />
            </button>
            <span className="approval-preview-label">Approval</span>
            <span className="preview-action">
              <Icon name="check" size={14} />
              Approve
            </span>
            <span className="preview-action">Deny</span>
            <span className="preview-action">Ask for context</span>
          </div>
        ) : null}
        {choices.length ? (
          <fieldset className="message-choices">
            <legend>Answer choices</legend>
            {choices.map((choice, i) => (
              <div className="choice-editor" key={i}>
                <span className="choice-circle" />
                <input
                  aria-label={`Choice ${i + 1}`}
                  placeholder={`Choice ${i + 1}`}
                  value={choice}
                  maxLength={500}
                  onChange={(e) =>
                    setChoices((old) =>
                      old.map((c, n) => (n === i ? e.target.value : c)),
                    )
                  }
                />
                <button
                  type="button"
                  className="icon-button"
                  aria-label={`Remove choice ${i + 1}`}
                  onClick={() =>
                    setChoices((old) => old.filter((_, n) => n !== i))
                  }
                >
                  <Icon name="close" size={14} />
                </button>
              </div>
            ))}
            <Button
              icon="plus"
              disabled={choices.length >= 20}
              onClick={() => setChoices((old) => [...old, ""])}
            >
              Add choice
            </Button>
          </fieldset>
        ) : null}
        <div className="compose-bottom">
          <div className="actions">
            {canAsk && !approval && !choices.length ? (
              <>
                <Button icon="plus" onClick={() => setApproval(true)}>
                  Request approval
                </Button>
                <Button icon="plus" onClick={() => setChoices(["", ""])}>
                  Add answer choices
                </Button>
              </>
            ) : null}
            {choices.length ? (
              <Button onClick={() => setChoices([])}>Remove choices</Button>
            ) : null}
            {tools}
          </div>
          <Button
            primary
            icon="arrow"
            type="submit"
            disabled={
              op.pending || !body.trim() || (!conversation && !targets.length)
            }
          >
            {op.pending ? "Sending..." : "Send"}
          </Button>
        </div>
      </Form>
    </div>
  );
}
export function Compose({
  initial = [],
  conversation = "",
  mode = "message",
  onClose,
}: {
  initial?: Target[];
  conversation?: string;
  mode?: string;
  multiple?: boolean;
  onClose: () => void;
}) {
  const me = useSession();
  const [targets, setTargets] = useState(initial),
    [picking, setPicking] = useState(!initial.length),
    [delivery, setDelivery] = useState<"individual" | "group">("individual"),
    [participants, setParticipants] = useState<Identity[]>([]);
  const target = targets[0];
  const selected = useCollection<Identity>(
    target?.kind === "person"
      ? "people"
      : target?.kind === "service"
        ? "services"
        : "agents",
    "",
    { id: target?.id || "" },
    !!target &&
      targets.length === 1 &&
      !conversation &&
      ["person", "agent", "service"].includes(target.kind),
  );
  return (
    <Modal
      title={conversation ? "Message" : "New message"}
      onClose={onClose}
      className="compose-dialog"
    >
      {!conversation ? (
        <div className="field">
          {picking ? (
            <TargetPicker
              value={targets}
              onChange={setTargets}
              onIdentitiesChange={setParticipants}
              multiple
              messaging
              allowGroups
            />
          ) : (
            <div className="recipient-summary">
              <strong>
                {selected.items[0]?.name ||
                  `${targets.length} recipient${targets.length === 1 ? "" : "s"}`}
              </strong>
              <Button onClick={() => setPicking(true)}>Change</Button>
            </div>
          )}
        </div>
      ) : null}
      {!conversation && targets.length > 1 ? (
        <div className="delivery-mode">
          <div role="group" aria-label="Message delivery">
            <button
              type="button"
              aria-pressed={delivery === "individual"}
              onClick={() => setDelivery("individual")}
            >
              <Icon name="people" size={15} />
              Send individually
            </button>
            <button
              type="button"
              aria-pressed={delivery === "group"}
              onClick={() => setDelivery("group")}
            >
              <Icon name="conversations" size={15} />
              Create group chat
            </button>
          </div>
          <small>
            {delivery === "individual"
              ? "Separate, private conversations with each recipient."
              : "Everyone can see and reply in the same conversation."}
          </small>
        </div>
      ) : null}
      <MessageComposer
        targets={targets}
        delivery={delivery}
        conversation={conversation}
        replyRequired={mode === "question"}
        participants={
          delivery === "group"
            ? [me.person, ...participants.filter((p) => p.id !== me.person.id)]
            : []
        }
        onSent={(id) => {
          onClose();
          if (id) navigate("/conversations/" + id);
        }}
      />
    </Modal>
  );
}
