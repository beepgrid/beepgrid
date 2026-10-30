import { useState } from "react";
import type { Conversation, Target } from "./contracts";
import { useOperation, useSession, clearConversationDrafts } from "./data";
import { TargetPicker } from "./TargetPicker";
import { Button, ErrorView, Field, Form, IdentityIcon, Modal } from "./ui";

export function ChatMembers({
  chat,
  onClose,
  onChanged,
  onLeft,
}: {
  chat: Conversation;
  onClose: () => void;
  onChanged: () => void;
  onLeft: () => void;
}) {
  const me = useSession(),
    op = useOperation();
  const [mode, setMode] = useState<
    "members" | "add" | "name" | "leave" | "remove"
  >("members");
  const [targets, setTargets] = useState<Target[]>([]),
    [share, setShare] = useState(false),
    [title, setTitle] = useState(chat.title),
    [removing, setRemoving] = useState(""),
    [successor, setSuccessor] = useState("");
  const manager = me.admin || chat.manager_id === me.person.id;
  const removingSelf = mode === "leave" || removing === me.person.id;
  const removesManager =
    mode === "leave"
      ? chat.manager_id === me.person.id
      : chat.manager_id === removing;
  const others = chat.participants.filter(
    (p) => p.id !== (mode === "leave" ? me.person.id : removing),
  );
  const name =
    chat.participants.find((p) => p.id === removing)?.name || "this member";
  async function save(operation: string, extra: object = {}) {
    const result = await op.run<{ left?: boolean }>(
      `/conversations/${chat.id}/membership`,
      {
        operation,
        version: chat.version,
        ...extra,
      },
    );
    if (!result) return;
    if (result.left) {
      clearConversationDrafts(chat.id);
      onLeft();
    } else {
      onChanged();
      onClose();
    }
  }
  return (
    <Modal
      title={
        mode === "add"
          ? "Add members"
          : mode === "name"
            ? "Chat name"
            : mode === "leave"
              ? "Leave chat"
              : mode === "remove"
                ? "Remove member"
                : "Chat details"
      }
      onClose={onClose}
      className="chat-members-dialog"
    >
      <ErrorView error={op.error} />
      {mode === "members" ? (
        <>
          <div className="chat-details-name">
            <div>
              <strong>{chat.title}</strong>
              <span>{chat.participants.length} members</span>
            </div>
            {manager ? (
              <Button onClick={() => setMode("name")}>Rename</Button>
            ) : null}
          </div>
          <div className="chat-member-list">
            {chat.participants.map((p) => (
              <div key={p.id} className="chat-member-row">
                <IdentityIcon kind={p.kind} id={p.id} label={p.name} />
                <div>
                  <strong>
                    {p.name}
                    {p.id === me.person.id ? " (you)" : ""}
                  </strong>
                  <span>
                    {p.id === chat.manager_id
                      ? "Chat manager"
                      : p.kind === "agent"
                        ? "Agent"
                        : p.kind === "service"
                          ? "Software service"
                          : "Person"}
                  </span>
                </div>
                {manager && p.id !== me.person.id ? (
                  <Button
                    onClick={() => {
                      setRemoving(p.id);
                      setMode("remove");
                    }}
                  >
                    Remove
                  </Button>
                ) : null}
              </div>
            ))}
          </div>
          <div className="chat-member-actions">
            {manager ? (
              <Button icon="plus" onClick={() => setMode("add")}>
                Add members
              </Button>
            ) : (
              <span />
            )}
            {chat.participants.some((p) => p.id === me.person.id) ? (
              <Button onClick={() => setMode("leave")}>Leave chat</Button>
            ) : null}
          </div>
        </>
      ) : mode === "add" ? (
        <>
          <TargetPicker
            value={targets}
            onChange={setTargets}
            multiple
            messaging
            allowGroups={false}
            allowInstances={false}
            excludeIds={chat.participants.map((p) => p.id)}
          />
          <label className="chat-history-choice">
            <input
              type="checkbox"
              checked={share}
              onChange={(e) => setShare(e.target.checked)}
            />
            <span>
              <strong>Share earlier history</strong>
              <small>
                {share
                  ? "New members will see earlier messages, decisions and files."
                  : "New members will see messages from the time they join."}
              </small>
            </span>
          </label>
          <div className="dialog-actions">
            <Button onClick={() => setMode("members")}>Back</Button>
            <Button
              primary
              disabled={!targets.length || op.pending}
              onClick={() =>
                void save("add", {
                  members: targets.map((t) => t.id),
                  share_history: share,
                })
              }
            >
              {op.pending ? "Adding..." : "Add members"}
            </Button>
          </div>
        </>
      ) : mode === "name" ? (
        <Form onSubmit={() => void save("rename", { title: title.trim() })}>
          <Field label="Chat name">
            <input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              maxLength={200}
              autoFocus
            />
          </Field>
          <div className="dialog-actions">
            <Button onClick={() => setMode("members")}>Back</Button>
            <Button
              primary
              type="submit"
              disabled={!title.trim() || op.pending}
            >
              Save name
            </Button>
          </div>
        </Form>
      ) : (
        <>
          <p className="chat-member-confirm">
            {removingSelf
              ? "You won’t receive new messages or have access to this chat."
              : `${name} will lose access to this chat and stop receiving its messages.`}
          </p>
          {removesManager && others.length ? (
            <Field label="New chat manager">
              <select
                value={successor}
                onChange={(e) => setSuccessor(e.target.value)}
              >
                <option value="">Choose a member</option>
                {others.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </Field>
          ) : null}
          <div className="dialog-actions">
            <Button onClick={() => setMode("members")}>Back</Button>
            <Button
              primary
              disabled={
                op.pending || (removesManager && !!others.length && !successor)
              }
              onClick={() =>
                void save(mode === "leave" ? "leave" : "remove", {
                  members: mode === "remove" ? [removing] : undefined,
                  next_manager: successor,
                })
              }
            >
              {mode === "leave" ? "Leave chat" : "Remove member"}
            </Button>
          </div>
        </>
      )}
    </Modal>
  );
}
