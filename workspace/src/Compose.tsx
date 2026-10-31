import { useState, useRef, type ReactNode } from "react";
import { useCollection, useDraft, useOperation, useSession } from "./data";
import { MentionEditor } from "./Mentions";
import type { Identity, Mention, Target, Attachment } from "./contracts";
import { bytes, upload } from "./client";
import { fileSize } from "./Attachments";
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
  type DraftFile = {
    key: string;
    name: string;
    size: number;
    id?: string;
    error?: string;
  };
  const [files, setFiles] = useState<DraftFile[]>([]);
  const fileInput = useRef<HTMLInputElement>(null);
  async function attach(chosen: FileList | null) {
    if (!chosen) return;
    const added = Array.from(chosen);
    if (added.length + files.length > 5) {
      setError(Error("Attach up to five files per message."));
      return;
    }
    for (const file of added) {
      if (!file.size || file.size > 5 * 1024 * 1024) {
        setError(Error("Choose a nonempty file up to 5 MiB."));
        continue;
      }
      if (
        ![
          "text/plain",
          "application/json",
          "application/pdf",
          "image/png",
          "image/jpeg",
          "image/webp",
        ].includes(file.type)
      ) {
        setError(
          Error("Supported types: text, JSON, PDF, PNG, JPEG and WebP."),
        );
        continue;
      }
      const key = crypto.randomUUID();
      setFiles((old) => [...old, { key, name: file.name, size: file.size }]);
      void upload<Attachment>("/attachments", file)
        .then((result) =>
          setFiles((old) =>
            old.map((x) => (x.key === key ? { ...x, id: result.id } : x)),
          ),
        )
        .catch((e) =>
          setFiles((old) =>
            old.map((x) =>
              x.key === key ? { ...x, error: e.message || "Upload failed" } : x,
            ),
          ),
        );
    }
    if (fileInput.current) fileInput.current.value = "";
  }
  const waitingFiles = files.some((x) => !x.id);
  const attachment_ids = files.flatMap((x) => (x.id ? [x.id] : []));
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
    if ((!body.trim() && !files.length) || waitingFiles || op.pending) return;
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
              attachment_ids,
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
          { targets, body, reuse_direct: true, attachment_ids },
        );
        if (sent) result = { conversation_id: sent[0]?.result.conversation_id };
      }
    } else if (!conversation && targets.length > 1) {
      result = await op.run("/conversations", {
        targets,
        body,
        attachment_ids,
        approval,
        choices: clean,
        mentions,
      });
    } else if (request) {
      result = await op.run("/requests", {
        attachment_ids,
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
        attachment_ids,
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
      setFiles([]);
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
        {files.length ? (
          <div className="draft-files" aria-label="Message attachments">
            {files.map((file) => (
              <div className="draft-file" key={file.key}>
                <Icon name="file" size={18} />
                <span className="file-label">
                  <strong>{file.name}</strong>
                  <small role={file.error ? "alert" : undefined}>
                    {file.error ||
                      (file.id ? fileSize(file.size) : "Uploading...")}
                  </small>
                </span>
                <button
                  type="button"
                  className="icon-button"
                  aria-label={"Remove " + file.name}
                  disabled={op.pending}
                  onClick={() =>
                    setFiles((old) => old.filter((x) => x.key !== file.key))
                  }
                >
                  <Icon name="close" size={14} />
                </button>
              </div>
            ))}
          </div>
        ) : null}
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
            <span className="approval-preview-label">Approval flow</span>
            <div className="approval-preview-actions">
              <span className="preview-action">Approve</span>
              <span className="preview-action">Decline</span>
              <span className="preview-action">Ask for context</span>
            </div>
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
            <Button
              icon="attachment"
              disabled={op.pending || files.length >= 5}
              onClick={() => fileInput.current?.click()}
            >
              Attach files
            </Button>
            <input
              hidden
              type="file"
              multiple
              ref={fileInput}
              aria-label="Choose files"
              accept=".txt,.json,.pdf,.png,.jpg,.jpeg,.webp"
              onChange={(e) => void attach(e.target.files)}
            />

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
              op.pending ||
              waitingFiles ||
              (!body.trim() && !files.length) ||
              (!conversation && !targets.length)
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
