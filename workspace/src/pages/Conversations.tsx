import {
  memo,
  useDeferredValue,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  usePostResource,
  clearConversationDrafts,
  accessUnavailable,
  useCollection,
  useResource,
  useSession,
} from "../data";
import type {
  Capability,
  Attachment,
  Conversation,
  Message,
  Identity,
  Request,
} from "../contracts";
import { ChatMembers } from "../ChatMembers";
import { FileLinks } from "../Attachments";
import { relative, timestamp } from "../client";
import { navigate, useRoute, workspaceURL } from "../navigation";
import { MentionText } from "../Mentions";
import { RequestCard } from "../RequestCard";
import { Compose, MessageComposer } from "../Compose";
import {
  Badge,
  Button,
  ConversationAvatar,
  ConversationPreview,
  isOutcomePreview,
  conversationPeers,
  conversationLabel,
  Empty,
  ErrorView,
  Icon,
  IdentityIcon,
  Loading,
  PageHeader,
  QueryState,
  RequestStatus,
  Search,
} from "../ui";

import Overseer from "./Overseer";
import { OverseerEntry } from "../OverseerEntry";
import { groupQuestions } from "../request-groups";

export default function Conversations() {
  const route = useRoute(),
    me = useSession();
  const [search, setSearch] = useState(""),
    [compose, setCompose] = useState(false),
    [view, setView] = useState("all");
  const q = useCollection<Conversation>(
    "conversations",
    useDeferredValue(search),
    { participant_id: me.person.id },
  );
  const attention = useCollection<Request>(
    "requests",
    "",
    { recipient_id: me.person.id, state: "open" },
    view === "attention",
  );
  return (
    <>
      <PageHeader
        title="Messages"
        actions={
          <Button primary icon="plus" onClick={() => setCompose(true)}>
            New message
          </Button>
        }
      />
      {route.id ? (
        <div className="mobile-chat-back">
          <Button onClick={() => navigate("/conversations")}>
            All messages
          </Button>
        </div>
      ) : null}
      <div className={`message-view ${route.id ? "has-selection" : ""}`}>
        <aside className="conversation-list" aria-label="Conversations">
          <Search
            value={search}
            onChange={setSearch}
            placeholder="Search messages"
          />
          <div className="message-filters">
            <button
              aria-pressed={view === "all"}
              onClick={() => setView("all")}
            >
              All messages
            </button>
            <button
              aria-pressed={view === "attention"}
              onClick={() => setView("attention")}
            >
              Needs attention
            </button>
          </div>
          {!search || "overseer".includes(search.toLowerCase()) ? (
            <OverseerEntry selected={route.id === "overseer"} />
          ) : null}
          {view === "attention" ? (
            <QueryState query={attention} empty="You’re all caught up">
              {groupQuestions(attention.items).map((group) => {
                const r = group[0];
                return (
                  <button
                    className="conversation-row"
                    key={r.id}
                    onClick={() =>
                      navigate("/conversations/" + r.conversation_id)
                    }
                  >
                    <strong>{r.body || r.title}</strong>
                    <p>
                      {r.kind === "approval"
                        ? "Approval requested"
                        : "Waiting for your reply"}
                      {group.length > 1 ? (
                        <span> · {group.length} matching questions</span>
                      ) : null}
                    </p>
                  </button>
                );
              })}
            </QueryState>
          ) : (
            <QueryState query={q} empty="Your conversations will appear here">
              {q.items.map((c) => (
                <button
                  className="conversation-row conversation-preview"
                  key={c.id}
                  aria-current={route.id === c.id}
                  onClick={() => navigate("/conversations/" + c.id)}
                >
                  <ConversationAvatar
                    participants={c.participants || []}
                    currentId={me.person.id}
                  />
                  <div className="conversation-copy">
                    <div className="conversation-row-title">
                      <strong>
                        {conversationPeers(c.participants || [], me.person.id)
                          .length === 1
                          ? conversationPeers(
                              c.participants || [],
                              me.person.id,
                            )[0].name
                          : c.title || "Group chat"}
                      </strong>
                      <time
                        dateTime={c.last_activity_at || c.created_at}
                        title={timestamp(c.last_activity_at || c.created_at)}
                      >
                        {relative(c.last_activity_at || c.created_at)}
                      </time>
                    </div>
                    {!isOutcomePreview(c.last_preview || "") ? (
                      <p>
                        <ConversationPreview
                          body={
                            c.last_preview ||
                            (c.shared
                              ? "No messages yet"
                              : c.title === "Conversation"
                                ? "Start a conversation"
                                : c.title)
                          }
                        />
                      </p>
                    ) : null}
                    <div className="conversation-meta">
                      <Badge>
                        {conversationLabel(c.participants || [], me.person.id)}
                      </Badge>
                      {isOutcomePreview(c.last_preview || "") ? (
                        <ConversationPreview body={c.last_preview!} />
                      ) : null}
                    </div>
                  </div>
                </button>
              ))}
            </QueryState>
          )}
        </aside>
        {route.id ? (
          route.id === "overseer" ? (
            <Overseer />
          ) : (
            <Chat key={route.id} id={route.id} />
          )
        ) : (
          <Empty title="Choose a conversation" icon="conversations">
            Start a message with a person, an Agent or a group of recipients.
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
  const [error, setError] = useState<Error | null>(null),
    [history, setHistory] = useState(false),
    [membersOpen, setMembersOpen] = useState(false);
  const timeline = useRef<HTMLDivElement>(null),
    scroll = useRef<HTMLDivElement>(null),
    pinned = useRef(true);
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
  // Reply routing must include open questions even when their history is on an
  // older page. This shares the existing query cache and live invalidation.
  const pending = useCollection<Request>(
    "requests",
    "",
    {
      conversation_id: id,
      recipient_id: me.person.id,
      state: "open",
      kind: "question",
    },
    !!q.data && !q.error,
  );
  const [selectedReply, setSelectedReply] = useState<string | null>(null);
  const incoming = pending.items.filter(
    (r) =>
      r.state === "open" &&
      r.kind === "question" &&
      !r.choices?.length &&
      r.requester_id !== me.person.id &&
      q.data?.participants.some((p) => p.id === me.person.id),
  );
  const replyTo =
    incoming.find((r) => r.id === selectedReply) ||
    (incoming.length === 1 && !pending.hasNextPage ? incoming[0] : undefined);
  const needsReplyChoice = incoming.length > 1 || pending.hasNextPage;
  const choosingReply =
    needsReplyChoice &&
    !replyTo &&
    !(selectedReply === "message" && caps.data?.operations.message);
  const replyCaps = usePostResource<Capability>(
    "/capabilities",
    { target: {}, request_id: replyTo?.id, connection_id: undefined },
    !!replyTo,
  );
  const replying = replyCaps.data?.operations.respond ? replyTo : undefined;
  const decisions = related.items.filter((r) => r.response && r.resolved_at);
  useLayoutEffect(() => {
    const el = scroll.current;
    if (pinned.current && el) el.scrollTop = el.scrollHeight;
  }, [
    messages.length,
    related.items.length,
    decisions.length,
    caps.isPending,
    id,
  ]);
  useEffect(() => {
    const el = scroll.current,
      content = timeline.current;
    if (!el || !content || typeof ResizeObserver === "undefined") return;
    // Follow layout changes only while reading the newest messages. Opening
    // approval controls or loading files must not obscure the last reply.
    const observer = new ResizeObserver(() => {
      if (pinned.current) el.scrollTop = el.scrollHeight;
    });
    observer.observe(el);
    observer.observe(content);
    return () => observer.disconnect();
  }, [!!q.data, id]);
  useEffect(() => {
    if (accessUnavailable(q.error) || accessUnavailable(caps.error))
      clearConversationDrafts(id);
    else if (caps.data && !caps.data.operations.message)
      clearConversationDrafts(id, false);
  }, [q.error, caps.error, caps.data, id]);
  if (q.isPending) return <Loading label="Opening conversation" />;
  if (!q.data)
    return <ErrorView error={q.error} retry={() => void q.refetch()} />;
  const c = q.data!;
  const knownParticipants = [...c.participants, ...(c.past_participants || [])];
  return (
    <section className="chat">
      <header className="chat-head">
        <ConversationAvatar
          participants={c.participants}
          currentId={me.person.id}
        />
        <div className="chat-heading">
          <strong>
            {participants.length === 1 && !c.shared
              ? participants[0].name
              : c.title || "Group chat"}
          </strong>
          <p>{c.participants.map((p) => p.name).join(", ")}</p>
        </div>
        {!c.shared &&
        participants.length === 1 &&
        participants[0].kind === "agent" ? (
          <Button
            primary
            onClick={() =>
              navigate("/agents/" + encodeURIComponent(participants[0].id))
            }
          >
            View Agent
          </Button>
        ) : null}
        {c.shared ? (
          <Button
            icon="people"
            aria-label={`${c.participants.length} members`}
            onClick={() => setMembersOpen(true)}
          >
            <span>
              {c.participants.length}
              <span className="chat-member-label"> members</span>
            </span>
          </Button>
        ) : null}
      </header>
      {membersOpen ? (
        <ChatMembers
          chat={c}
          onClose={() => setMembersOpen(false)}
          onChanged={() => void q.refetch()}
          onLeft={() => {
            setMembersOpen(false);
            cache.removeQueries({
              predicate: (q) => JSON.stringify(q.queryKey).includes(id),
            });
            navigate("/conversations");
          }}
        />
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
        <div className="message-timeline" ref={timeline}>
          {history ? (
            <Button
              onClick={() => void all.fetchNextPage()}
              disabled={!all.hasNextPage || all.isFetchingNextPage}
            >
              Earlier messages
            </Button>
          ) : messages.length >= 500 ? (
            <Button
              onClick={() => {
                pinned.current = false;
                setHistory(true);
              }}
            >
              Earlier messages
            </Button>
          ) : null}
          {[
            ...messages.map((m) => ({
              kind: "message" as const,
              at: m.created_at,
              id: m.id,
              value: m,
            })),
            ...related.items.map((r) => ({
              kind: "request" as const,
              at: r.created_at,
              id: r.id,
              value: r,
            })),
            ...decisions.map((r) => ({
              kind: "decision" as const,
              at: r.resolved_at!,
              id: r.id + "/decision",
              value: r,
            })),
          ]
            .sort(
              (a, b) => a.at.localeCompare(b.at) || a.id.localeCompare(b.id),
            )
            .map((item) =>
              item.kind === "message" ? (
                <ChatMessage
                  key={item.id}
                  message={item.value}
                  participants={knownParticipants}
                  name={
                    knownParticipants.find((p) => p.id === item.value.sender_id)
                      ?.name || "Participant"
                  }
                  kind={
                    knownParticipants.find((p) => p.id === item.value.sender_id)
                      ?.kind || "agent"
                  }
                />
              ) : item.kind === "decision" ? (
                <article key={item.id} className="message">
                  <IdentityIcon
                    kind={
                      knownParticipants.find(
                        (p) => p.id === item.value.resolved_by,
                      )?.kind || "person"
                    }
                    id={item.value.resolved_by}
                    icon={String(
                      knownParticipants.find(
                        (p) => p.id === item.value.resolved_by,
                      )?.metadata?.appearance_icon || "",
                    )}
                    label={
                      knownParticipants.find(
                        (p) => p.id === item.value.resolved_by,
                      )?.name
                    }
                  />
                  <div className="message-body">
                    <div className="message-meta">
                      <strong>
                        {knownParticipants.find(
                          (p) => p.id === item.value.resolved_by,
                        )?.name || "Participant"}
                      </strong>
                      <time>
                        {new Date(item.at).toLocaleTimeString(undefined, {
                          hour: "2-digit",
                          minute: "2-digit",
                        })}
                      </time>
                    </div>
                    <p
                      className={
                        item.value.response?.outcome === "approved"
                          ? "approved-reply"
                          : ""
                      }
                    >
                      <RequestStatus request={item.value} />
                      {typeof item.value.response?.answer === "string" &&
                      item.value.response.answer ? (
                        <span className="decision-answer">
                          {item.value.response.answer}
                        </span>
                      ) : null}
                    </p>
                  </div>
                </article>
              ) : (
                <RequestCard
                  key={item.id}
                  request={item.value}
                  participants={knownParticipants}
                  sender={knownParticipants.find(
                    (p) => p.id === item.value.requester_id,
                  )}
                  conversational
                  onFollowUp={() =>
                    document
                      .querySelector<HTMLTextAreaElement>(".composer textarea")
                      ?.focus()
                  }
                />
              ),
            )}
          {!messages.length && !related.items.length && !related.isPending ? (
            <Empty
              title={
                c.history_start
                  ? "You’re in the chat"
                  : "Start the conversation"
              }
              icon="conversations"
            >
              {c.history_start
                ? "Messages sent after you joined will appear here."
                : null}
            </Empty>
          ) : null}
          {related.hasNextPage ? (
            <Button onClick={() => void related.fetchNextPage()}>
              More decisions
            </Button>
          ) : null}
        </div>
      </div>
      {attachments.error ? <ErrorView error={attachments.error} /> : null}
      {attachments.items.some((a) => !a.message_id && !a.request_id) ? (
        <div className="attachments">
          {attachments.items
            .filter((a) => !a.message_id && !a.request_id)
            .map((a) => (
              <a
                className="file-chip"
                key={a.id}
                href={workspaceURL("/api/v1/attachments/" + a.id)}
                download
              >
                <Icon name="file" size={14} />
                {a.file_name}
                <small>{(a.size / 1024).toFixed(1)} KiB</small>
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
        <ErrorView error={error} />
        {needsReplyChoice ? (
          <div className="reply-target">
            <span>Reply to</span>
            <select
              aria-label="Question to reply to"
              value={
                replyTo?.id ||
                (selectedReply === "message" && caps.data?.operations.message
                  ? "message"
                  : "")
              }
              onChange={(e) => setSelectedReply(e.target.value)}
            >
              <option value="" disabled>
                Choose a question
              </option>
              {incoming.map((r) => (
                <option key={r.id} value={r.id}>
                  {knownParticipants.find((p) => p.id === r.requester_id)
                    ?.name || "Participant"}
                  : {(r.body || r.title).slice(0, 100)}
                </option>
              ))}
              {caps.data?.operations.message ? (
                <option value="message">Send a separate message</option>
              ) : null}
            </select>
            {pending.hasNextPage ? (
              <Button
                disabled={pending.isFetchingNextPage}
                onClick={() => void pending.fetchNextPage()}
              >
                More questions
              </Button>
            ) : null}
          </div>
        ) : null}
        {pending.error ? (
          <ErrorView
            error={pending.error}
            retry={() => void pending.refetch()}
          />
        ) : related.error ? (
          <ErrorView
            error={related.error}
            retry={() => void related.refetch()}
          />
        ) : caps.isPending ||
          related.isPending ||
          pending.isPending ||
          (replyTo && replyCaps.isPending) ? (
          <Loading label="Checking conversation access" />
        ) : replyTo && !replying ? (
          replyCaps.error ? (
            <ErrorView
              error={replyCaps.error}
              retry={() => void replyCaps.refetch()}
            />
          ) : (
            <p>Replying to this question is no longer available.</p>
          )
        ) : replying ||
          choosingReply ||
          (caps.data?.operations.message && destination) ? (
          <MessageComposer
            key={replying?.id || "message"}
            replyTo={replying}
            disabled={choosingReply}
            conversation={id}
            participants={c.participants}
            targets={destination ? [destination] : []}
            canAsk={!!caps.data?.operations.ask}
            onSent={() => {
              pinned.current = true;
              setSelectedReply(null);
            }}
          />
        ) : (
          <p>Messaging is unavailable for this conversation.</p>
        )}
      </div>
    </section>
  );
}
const ChatMessage = memo(function ChatMessage({
  message: m,
  name,
  kind,
  participants,
}: {
  participants: Identity[];
  message: Message;
  name: string;
  kind: string;
}) {
  return (
    <article className="message">
      <IdentityIcon
        id={m.sender_id}
        kind={kind}
        label={name}
        icon={String(
          participants.find((p) => p.id === m.sender_id)?.metadata
            ?.appearance_icon || "",
        )}
      />
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
        <p>
          <MentionText
            body={m.body}
            mentions={m.mentions}
            participants={participants}
          />
        </p>
        <FileLinks files={m.attachments} />
      </div>
    </article>
  );
});
