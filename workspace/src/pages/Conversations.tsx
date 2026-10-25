import { memo, useDeferredValue, useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  usePostResource,
  useCollection,
  useDraft,
  useOperation,
  useResource,
  useSession,
} from "../data";
import type {
  Capability,
  Attachment,
  Conversation,
  Message,
  Request,
} from "../contracts";
import { bytes, timestamp, upload } from "../client";
import { navigate, useRoute } from "../navigation";
import { Compose } from "../Compose";
import {
  Button,
  DateValue,
  Details,
  Empty,
  ErrorView,
  Field,
  Form,
  Icon,
  IdentityIcon,
  Loading,
  PageHeader,
  QueryState,
  Search,
} from "../ui";

export default function Conversations() {
  const route = useRoute();
  const [search, setSearch] = useState(""),
    [compose, setCompose] = useState(false);
  const q = useCollection<Conversation>(
    "conversations",
    useDeferredValue(search),
  );
  return (
    <>
      <PageHeader
        title="Conversations"
        actions={
          <Button primary icon="plus" onClick={() => setCompose(true)}>
            New conversation
          </Button>
        }
      />
      {route.id ? (
        <div className="mobile-chat-back">
          <Button onClick={() => navigate("/conversations")}>
            All conversations
          </Button>
        </div>
      ) : null}
      <div className={`message-view ${route.id ? "has-selection" : ""}`}>
        <aside className="conversation-list" aria-label="Conversations">
          <Search
            value={search}
            onChange={setSearch}
            placeholder="Find a conversation"
          />
          <QueryState query={q} empty="No permitted conversations">
            {q.items.map((c) => (
              <button
                className="conversation-row"
                key={c.id}
                aria-current={route.id === c.id}
                onClick={() => navigate("/conversations/" + c.id)}
              >
                <strong>{c.title || "Untitled conversation"}</strong>
                <p>{c.participants?.map((p) => p.name).join(", ") || c.id}</p>
              </button>
            ))}
          </QueryState>
        </aside>
        {route.id ? (
          <Chat key={route.id} id={route.id} />
        ) : (
          <Empty title="Choose a conversation" icon="conversations">
            Messages stay with their authorized participants.
          </Empty>
        )}
      </div>
      {compose ? <Compose onClose={() => setCompose(false)} /> : null}
    </>
  );
}
function Chat({ id }: { id: string }) {
  const me = useSession(),
    q = useResource<Conversation>("/conversations/" + id),
    cache = useQueryClient();
  const [body, setBody] = useDraft("conversation:" + id),
    [error, setError] = useState<Error | null>(null),
    [uploading, setUploading] = useState(false),
    [history, setHistory] = useState(false),
    [ask, setAsk] = useState(false);
  const op = useOperation("conversation:" + id),
    end = useRef<HTMLDivElement>(null),
    scroll = useRef<HTMLDivElement>(null),
    pinned = useRef(true),
    fileInput = useRef<HTMLInputElement>(null);
  const participants =
    q.data?.participants?.filter((p) => p.id !== me.person.id) || [];
  const destination = participants[0] && {
    kind: participants[0].kind,
    id: participants[0].id,
  };
  const caps = usePostResource<Capability>(
    "/capabilities",
    { conversation_id: id },
    !!q.data && !q.error,
  );
  const attachments = useCollection<Attachment>(
    "attachments",
    "",
    { conversation_id: id },
    !!q.data && !q.error,
  );
  const all = useCollection<Message>(
    "messages",
    "",
    { conversation_id: id },
    history && !!q.data && !q.error,
  );
  const messages = history ? all.items : q.data?.messages || [];
  const related = useCollection<Request>(
    "requests",
    "",
    { conversation_id: id },
    !!q.data && !q.error,
  );
  const [showRequests, setShowRequests] = useState(false);
  useEffect(() => {
    if (pinned.current) end.current?.scrollIntoView({ block: "end" });
  }, [messages.length, id]);
  useEffect(() => {
    if (q.error || caps.error || (caps.data && !caps.data.operations.message))
      setBody("");
  }, [q.error, caps.error, caps.data, setBody]);
  async function send() {
    setError(null);
    if (!destination || !body.trim()) return;
    if (bytes(body) > 32768) {
      setError(Error("Messages can contain at most 32,768 UTF-8 bytes."));
      return;
    }
    const result = await op.run<{
      messages: Message[];
      conversation_id: string;
    }>("/messages", { target: destination, conversation_id: id, body });
    if (result) {
      setBody("");
      pinned.current = true;
    }
  }
  async function attach(file?: File) {
    if (!file) return;
    setError(null);
    if (!file.size || file.size > 5 * 1024 * 1024) {
      setError(Error("Choose a nonempty file up to 5 MiB."));
      return;
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
      setError(Error("Supported types: text, JSON, PDF, PNG, JPEG and WebP."));
      return;
    }
    setUploading(true);
    try {
      await upload("/conversations/" + id + "/attachments", file);
      void cache.invalidateQueries();
    } catch (e) {
      setError(e as Error);
    } finally {
      setUploading(false);
      if (fileInput.current) fileInput.current.value = "";
    }
  }
  if (q.isPending) return <Loading label="Opening conversation" />;
  if (q.error)
    return <ErrorView error={q.error} retry={() => void q.refetch()} />;
  const c = q.data!;
  return (
    <section className="chat">
      <header className="chat-head">
        <IdentityIcon
          kind={participants[0]?.kind}
          label={participants[0]?.name}
        />
        <div className="chat-heading">
          <strong>{c.title || "Conversation"}</strong>
          <p>{c.participants.map((p) => p.name).join(", ")}</p>
        </div>
        {caps.data?.operations.ask ? (
          <Button onClick={() => setAsk(true)}>New request</Button>
        ) : null}
        <Button onClick={() => setShowRequests((x) => !x)}>Requests</Button>
        <Details
          value={{
            id: c.id,
            participants: c.participants.map((p) => ({
              kind: p.kind,
              name: p.name,
              id: p.id,
            })),
            created_at: c.created_at,
          }}
          label="Context"
        />
      </header>
      {showRequests ? (
        <section className="conversation-requests">
          <h3>Related requests</h3>
          <QueryState
            query={related}
            empty="No readable requests in this conversation"
          >
            {related.items.map((r) => (
              <button
                key={r.id}
                className="conversation-row"
                onClick={() => navigate("/requests/" + r.id)}
              >
                <strong>{r.title}</strong>
                <p>
                  {r.kind} · {r.response?.outcome || r.state} · {r.id}
                </p>
              </button>
            ))}
          </QueryState>
        </section>
      ) : null}
      <div
        className="messages"
        ref={scroll}
        onScroll={() => {
          const el = scroll.current!;
          pinned.current =
            el.scrollHeight - el.scrollTop - el.clientHeight < 100;
        }}
      >
        {history ? (
          <QueryState query={all} empty="No readable messages">
            {messages.map((m) => (
              <ChatMessage
                key={m.id}
                message={m}
                name={
                  c.participants.find((p) => p.id === m.sender_id)?.name ||
                  m.sender_id
                }
                kind={
                  c.participants.find((p) => p.id === m.sender_id)?.kind ||
                  "agent"
                }
              />
            ))}
          </QueryState>
        ) : (
          <>
            <div style={{ textAlign: "center", marginBottom: 25 }}>
              <Button
                onClick={() => {
                  pinned.current = false;
                  setHistory(true);
                }}
              >
                Browse complete paged history
              </Button>
              <p>
                <small>
                  Recent messages shown, up to 500. History uses your current
                  access.
                </small>
              </p>
            </div>
            {messages.length === 0 ? (
              <Empty title="No messages yet" icon="conversations" />
            ) : (
              messages.map((m) => (
                <ChatMessage
                  key={m.id}
                  message={m}
                  name={
                    c.participants.find((p) => p.id === m.sender_id)?.name ||
                    m.sender_id
                  }
                  kind={
                    c.participants.find((p) => p.id === m.sender_id)?.kind ||
                    "agent"
                  }
                />
              ))
            )}
          </>
        )}
        <div ref={end} />
      </div>
      {attachments.error ? <ErrorView error={attachments.error} /> : null}
      {attachments.items.length ? (
        <div className="attachments">
          {attachments.items.map((a) => (
            <a
              className="file-chip"
              key={a.id}
              href={"/api/v1/attachments/" + a.id}
              download
            >
              <Icon name="file" size={14} />
              {a.file_name}
              <small>{(a.size / 1024).toFixed(1)} KiB</small>
              <span className="attachment-meta">
                {a.media_type} · {timestamp(a.created_at)} · {a.id} · SHA256{" "}
                {a.sha256}
              </span>
            </a>
          ))}
          {attachments.hasNextPage ? (
            <Button onClick={() => void attachments.fetchNextPage()}>
              More files
            </Button>
          ) : null}
        </div>
      ) : null}
      <div className="composer">
        <ErrorView error={error || op.error || caps.error} />
        {caps.data?.operations.message ? (
          <Form onSubmit={() => void send()}>
            {participants.length > 1 ? (
              <p className="help">
                Reply goes to the current conversation participants:{" "}
                {participants.map((p) => p.name).join(", ")}. Current permission
                is checked for each recipient.
              </p>
            ) : null}
            <textarea
              aria-label="Message"
              placeholder="Write a message…"
              rows={3}
              value={body}
              onChange={(e) => setBody(e.target.value)}
              onKeyDown={(e) => {
                if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
                  e.preventDefault();
                  void send();
                }
              }}
            />
            <div className="actions">
              <div className="actions">
                <button
                  className="icon-button"
                  type="button"
                  disabled={uploading}
                  aria-label="Attach a file"
                  onClick={() => fileInput.current?.click()}
                >
                  <Icon name="attachment" size={18} />
                </button>
                <input
                  hidden
                  type="file"
                  ref={fileInput}
                  accept=".txt,.json,.pdf,.png,.jpg,.jpeg,.webp"
                  onChange={(e) => void attach(e.target.files?.[0])}
                />
                <small>
                  {uploading
                    ? "Uploading attachment"
                    : "⌘ / Ctrl + Enter to send"}
                </small>
              </div>
              <Button
                primary
                type="submit"
                icon="arrow"
                disabled={op.pending || uploading || !body.trim()}
              >
                {op.pending ? "Sending" : "Send"}
              </Button>
            </div>
          </Form>
        ) : (
          <p>Messaging is unavailable under your current permissions.</p>
        )}
      </div>
      {ask && destination ? (
        <Compose
          mode="question"
          initial={[destination]}
          conversation={id}
          onClose={() => setAsk(false)}
        />
      ) : null}
    </section>
  );
}
const ChatMessage = memo(function ChatMessage({
  message: m,
  name,
  kind,
}: {
  message: Message;
  name: string;
  kind: string;
}) {
  return (
    <article className="message">
      <IdentityIcon kind={kind} label={name} />
      <div className="message-body">
        <div className="message-meta">
          <strong>{name}</strong>
          <time dateTime={m.created_at} title={timestamp(m.created_at)}>
            {new Date(m.created_at).toLocaleTimeString(undefined, {
              hour: "2-digit",
              minute: "2-digit",
            })}
          </time>
        </div>
        <p>{m.body}</p>
        {m.instance_id ? (
          <Details
            value={{
              sender_id: m.sender_id,
              instance_id: m.instance_id,
              created_at: m.created_at,
            }}
            label="Authenticated runtime"
          />
        ) : null}
      </div>
    </article>
  );
});
