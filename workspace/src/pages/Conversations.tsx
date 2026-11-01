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
  Page,
} from "../contracts";
import { ChatMembers } from "../ChatMembers";
import { FileLinks } from "../Attachments";
import { relative, timestamp } from "../client";
import { navigate, useRoute, workspaceURL } from "../navigation";
import { MentionText } from "../Mentions";
import { RequestCard } from "../RequestCard";
import { PendingQuestions } from "../PendingQuestions";
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
  RequestKind,
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
                  <AttentionRow
                    key={r.id}
                    group={group}
                    participants={q.items.flatMap((c) => c.participants || [])}
                  />
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
function AttentionRow({
  group,
  participants,
}: {
  group: Request[];
  participants: Identity[];
}) {
  const r = group[0];
  const me = useSession();
  const kind = (id: string) =>
    id.startsWith("agt_")
      ? "agent"
      : id.startsWith("svc_")
        ? "service"
        : "person";
  const known = participants.find((p) => p.id === r.requester_id);
  const profile = usePostResource<Page<Identity>>(
    "/query",
    {
      collection:
        kind(r.requester_id) === "agent"
          ? "agents"
          : kind(r.requester_id) === "service"
            ? "services"
            : "people",
      filters: { id: r.requester_id },
      limit: 1,
    },
    !known,
  );
  const author = known || profile.data?.items[0];
  const senders = [
    ...new Map(
      group.map((item) => {
        const person = participants.find((p) => p.id === item.requester_id) ||
          (item.requester_id === author?.id ? author : undefined) || {
            id: item.requester_id,
            kind: kind(item.requester_id),
            name: "Contact",
            metadata: {},
            external_id: "",
            disabled: false,
            created_at: item.created_at,
          };
        return [person.id, person] as const;
      }),
    ).values(),
  ];
  return (
    <button
      className="conversation-row conversation-preview"
      onClick={() => navigate("/conversations/" + r.conversation_id)}
    >
      <ConversationAvatar participants={senders} currentId={me.person.id} />
      <div className="conversation-copy">
        <div className="conversation-row-title">
          <strong>
            {author?.name || "Workspace contact"}
            {senders.length > 1 ? ` + ${senders.length - 1}` : ""}
          </strong>
          <time dateTime={r.created_at} title={timestamp(r.created_at)}>
            {relative(r.created_at)}
          </time>
        </div>
        <p>{r.body || r.title}</p>
        <div className="conversation-meta">
          <RequestKind kind={r.kind} />
          {group.length > 1 ? (
            <span>{group.length} matching questions</span>
          ) : null}
        </div>
      </div>
    </button>
  );
}

function Chat({ id }: { id: string }) {
  const me = useSession(),
    q = useResource<Conversation>("/conversations/" + id),
    cache = useQueryClient();
  const [error, setError] = useState<Error | null>(null),
    [history, setHistory] = useState(false),
    [membersOpen, setMembersOpen] = useState(false),
    [jumpTo, setJumpTo] = useState<string | null>(null);
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
  const approvals = useCollection<Request>(
    "requests",
    "",
    {
      conversation_id: id,
      recipient_id: me.person.id,
      state: "open",
      kind: "approval",
    },
    !!q.data && !q.error,
  );
  const incoming = [
    ...new Map(
      [...pending.items, ...approvals.items].map((r) => [r.id, r]),
    ).values(),
  ].filter(
    (r) =>
      r.state === "open" &&
      (r.kind === "question" || r.kind === "approval") &&
      r.requester_id !== me.person.id &&
      q.data?.participants.some((p) => p.id === me.person.id),
  );
  useEffect(() => {
    if (!jumpTo) return;
    const card = document.getElementById("request-" + jumpTo);
    if (card) {
      card.scrollIntoView?.({
        block: "center",
        behavior: matchMedia("(prefers-reduced-motion:reduce)").matches
          ? "instant"
          : "smooth",
      });
      card.focus({ preventScroll: true });
      setJumpTo(null);
    } else if (related.error || (!related.hasNextPage && !related.isFetching)) {
      setError(Error("This request is no longer available in the chat."));
      setJumpTo(null);
    } else if (related.hasNextPage && !related.isFetchingNextPage)
      void related.fetchNextPage();
  }, [
    jumpTo,
    related.items,
    related.hasNextPage,
    related.isFetchingNextPage,
    related.isFetching,
    related.error,
  ]);
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
  function jumpToRequest(requestId: string) {
    pinned.current = false;
    setError(null);
    setJumpTo(requestId);
  }
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
                      <time dateTime={item.at} title={timestamp(item.at)}>
                        {new Date(item.at).toLocaleTimeString(undefined, {
                          hour: "2-digit",
                          minute: "2-digit",
                        })}
                      </time>
                    </div>
                    <ReplyContext
                      request={item.value}
                      onJump={() => jumpToRequest(item.value.id)}
                    />
                    <div className="decision-response">
                      {item.value.response?.outcome !== "answered" ? (
                        <RequestStatus request={item.value} />
                      ) : null}
                      {typeof item.value.response?.answer === "string" &&
                      item.value.response.answer ? (
                        <p className="decision-answer">
                          {item.value.response.answer}
                        </p>
                      ) : null}
                    </div>
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
                      .querySelector<HTMLTextAreaElement>(
                        ".composer .message-input",
                      )
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
        {pending.error ? (
          <ErrorView
            error={pending.error}
            retry={() => void pending.refetch()}
          />
        ) : null}
        {approvals.error ? (
          <ErrorView
            error={approvals.error}
            retry={() => void approvals.refetch()}
          />
        ) : null}
        <PendingQuestions
          key={id}
          questions={pending.isPending || approvals.isPending ? [] : incoming}
          participants={knownParticipants}
          hasMore={pending.hasNextPage || approvals.hasNextPage}
          loadingMore={
            pending.isFetchingNextPage || approvals.isFetchingNextPage
          }
          onMore={() => {
            if (pending.hasNextPage) void pending.fetchNextPage();
            if (approvals.hasNextPage) void approvals.fetchNextPage();
          }}
          onJump={(request) => jumpToRequest(request.id)}
          onFollowUp={() =>
            document
              .querySelector<HTMLTextAreaElement>(".composer .message-input")
              ?.focus()
          }
        />
        {caps.isPending ? (
          <Loading label="Checking conversation access" />
        ) : caps.error && !caps.data ? (
          <ErrorView error={caps.error} retry={() => void caps.refetch()} />
        ) : caps.data?.operations.message && destination ? (
          <MessageComposer
            conversation={id}
            participants={c.participants}
            targets={[destination]}
            canAsk={!!caps.data?.operations.ask}
            onSent={() => {
              pinned.current = true;
            }}
          />
        ) : (
          <p>Messaging is unavailable for this conversation.</p>
        )}
      </div>
    </section>
  );
}
function ReplyContext({
  request,
  onJump,
}: {
  request: Request;
  onJump: () => void;
}) {
  const text = (
    request.body.trim() ||
    request.title.trim() ||
    "Request"
  ).replace(/\s+/g, " ");
  const characters = Array.from(text);
  const preview =
    characters.length > 120
      ? characters.slice(0, 119).join("").trimEnd() + "…"
      : text;
  return (
    <button
      type="button"
      className="reply-context"
      title="Show in chat"
      aria-label={`Show original ${request.kind} in chat: ${text}`}
      onClick={onJump}
    >
      <Icon name="reply" size={16} />
      <span>{preview}</span>
    </button>
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
  const me = useSession();
  const agents = participants.filter(
    (p) => p.kind === "agent" && p.id !== m.sender_id,
  );
  const acknowledged =
    m.sender_id === me.person.id &&
    agents.length > 0 &&
    agents.every((agent) => m.received_by?.includes(agent.id));
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
      <div className={`message-body${acknowledged ? " has-receipt" : ""}`}>
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
        {acknowledged ? (
          <span
            className="message-receipt"
            role="img"
            aria-label="Agent runtime acknowledged receipt"
            title="Acknowledged by the Agent runtime. This does not confirm that the model read it or completed the work."
          >
            <Icon name="checks" size={18} />
          </span>
        ) : null}
      </div>
    </article>
  );
});
