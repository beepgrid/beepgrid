import {
  useEffect,
  useId,
  useRef,
  useState,
  type ReactNode,
  type FormEvent,
} from "react";
import { APIError, readable, timestamp } from "./client";
import type { Identity, Request } from "./contracts";
import { AgentMark, identitySeed } from "./AgentMark";

const paths: Record<string, string> = {
  send: "m22 2-7 20-4-9-9-4z M22 2 11 13",
  mail: "M3 5h18v14H3z m0 0 9 7 9-7",
  slack: "M9 3v12 M3 9h12 M15 21V9 M21 15H9 M5 4h1 M18 4v2 M20 18h-2 M4 18v2",
  webhook:
    "M7 5a4 4 0 1 0 5 6 M8 17a4 4 0 1 0-1 4 M17 12a4 4 0 1 0 4 4 M8 8l5 8 M8 18h9 M17 8l-4 8",
  inbox: "M4 4h16v16H4z M4 13h5l2 3h2l2-3h5",
  conversations: "M4 4h16v13H9l-5 4z",
  agents: "m12 3 8 5v8l-8 5-8-5V8z M4 8l8 5 8-5 M12 13v8",
  people:
    "M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2 M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8 M20 21v-2a4 4 0 0 0-3-3.87 M16 3.13a4 4 0 0 1 0 7.75",
  groups: "M3 4h7l2 3h9v13H3z",
  permissions: "m12 2 8 4v6c0 5-8 10-8 10S4 17 4 12V6z M8 12l3 3 5-6",
  integrations: "M9 3v5m6-5v5 M6 8h12v3a6 6 0 0 1-6 6v5 M8 5h8",
  fleet: "M3 4h6v6H3z M15 4h6v6h-6z M9 16h6v6H9z M6 10v3h12v-3 M12 13v3",
  activity: "M3 12h4l3-8 4 16 3-8h4",
  settings:
    "M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8 M9 3h6l1 3 3 1 2 5-2 5-3 1-1 3H9l-1-3-3-1-2-5 2-5 3-1z",
  search: "M21 21l-5-5 M10 3a7 7 0 1 0 0 14 7 7 0 0 0 0-14",
  plus: "M12 5v14 M5 12h14",
  chevron: "m9 5 7 7-7 7",
  close: "m6 6 12 12 M6 18 18 6",
  sun: "M12 8a4 4 0 1 0 0 8 4 4 0 0 0 0-8 M12 2v2m0 16v2M2 12h2m16 0h2 M5 5l1.5 1.5m11 11L19 19 M5 19l1.5-1.5m11-11L19 5",
  moon: "M21 13A9 9 0 0 1 11 3a9 9 0 1 0 10 10",
  arrow: "M4 12h16m-6-6 6 6-6 6",
  check: "m5 12 4 4L19 6",
  logout: "M9 3H4v18h5 M10 12h11m-5-5 5 5-5 5",
  service: "M3 4h18v6H3z M3 14h18v6H3z M6 7h.01M6 17h.01",
  question:
    "M9 8a3 3 0 0 1 6 0c0 3-3 2-3 5 M12 17h.01 M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20",
  task: "M9 3h6v4H9z M9 5H5v16h14V5h-4 M8 12h8m-8 4h5",
  download: "M12 3v12m-5-5 5 5 5-5 M5 17v4h14v-4",
  attachment: "m8 12 6-6a3 3 0 0 1 4 4L9 19a5 5 0 0 1-7-7L12 2",
  menu: "M4 6h16M4 12h16M4 18h16",
  refresh: "M20 7v5h-5 M4 17v-5h5 M6 6a8 8 0 0 1 14 6M4 12a8 8 0 0 0 14 6",
  bolt: "m13 2-9 12h7l-1 8 10-13h-8z",
  code: "m8 6-6 6 6 6 M16 6l6 6-6 6 M14 3l-4 18",
  file: "M5 2h9l5 5v15H5z M14 2v6h5",
  link: "m10 13 4-4 M8 16l-2 2a4 4 0 0 1-6-6l5-5a4 4 0 0 1 6 0 M16 8l2-2a4 4 0 0 1 6 6l-5 5a4 4 0 0 1-6 0",
  copy: "M8 8h13v13H8z M16 8V3H3v13h5",
  globe:
    "M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20 M2 12h20 M12 2c6 7 6 13 0 20-6-7-6-13 0-20",
  eye: "M2 12s4-7 10-7 10 7 10 7-4 7-10 7-10-7-10-7z M12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6",
  key: "M8 3a5 5 0 1 0 0 10 5 5 0 0 0 0-10 M12 12l9 9m-3-3 3-3m-6 0 3-3",
  spark: "m12 2 3 7 7 3-7 3-3 7-3-7-7-3 7-3z",
  monitor: "M3 3h18v14H3z M8 22h8m-4-5v5",
  filter: "M3 4h18l-7 8v8l-4-2v-6z",
  lock: "M6 10h12v11H6z M8 10V6a4 4 0 0 1 8 0v4 M12 14v3",
  info: "M12 11v6 M12 7h.01 M12 2a10 10 0 1 0 0 20 10 10 0 0 0 0-20",
};
export function Icon({ name, size = 18 }: { name: string; size?: number }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.55"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d={paths[name] || paths.agents} />
    </svg>
  );
}
export function Logo({ word = true }: { word?: boolean }) {
  return (
    <span className="brand">
      <svg
        width="23"
        height="23"
        viewBox="0 0 24 24"
        fill="currentColor"
        aria-hidden="true"
      >
        {[0, 1, 2].flatMap((x) =>
          [0, 1, 2].map((y) => (
            <circle
              key={`${x}-${y}`}
              cx={4 + x * 8}
              cy={4 + y * 8}
              r={2.6}
              opacity={[0.24, 0.56, 1][x]}
            />
          )),
        )}
      </svg>
      {word ? <span>beepgrid</span> : null}
    </span>
  );
}
export function IdentityIcon({
  kind = "agent",
  label,
  id,
}: {
  kind?: string;
  label?: string;
  id?: string;
}) {
  const seed = identitySeed(`${kind}:${id || label || "unassigned"}`);
  const glyph =
    kind === "person"
      ? "people"
      : kind === "service"
        ? "service"
        : kind === "group"
          ? "groups"
          : kind === "instance"
            ? "code"
            : "conversations";
  return (
    <span
      className={`identity-icon identity-${kind}`}
      data-tone={
        ["blue", "teal", "amber", "slate", "copper", "forest"][seed % 6]
      }
      aria-hidden="true"
    >
      {kind === "agent" ? (
        <AgentMark seed={seed} />
      ) : kind === "person" && label ? (
        initials(label)
      ) : (
        <Icon name={glyph} />
      )}
    </span>
  );
}
function initials(name: string) {
  return name
    .trim()
    .split(/\s+/)
    .slice(0, 2)
    .map((word) => Array.from(word)[0])
    .join("")
    .toLocaleUpperCase();
}
type Participant = Pick<Identity, "id" | "kind" | "name">;
export function conversationPeers(
  participants: readonly Participant[],
  currentId: string,
) {
  const unique = participants.filter(
    (person, index, all) => all.findIndex((p) => p.id === person.id) === index,
  );
  const others = unique.filter((person) => person.id !== currentId);
  return others.length ? others : unique;
}
export function conversationLabel(
  participants: readonly Participant[],
  currentId: string,
) {
  const peers = conversationPeers(participants, currentId);
  return peers.length > 1
    ? `${new Set(participants.map((person) => person.id)).size} participants`
    : peers[0]?.kind || "Conversation";
}
export function ConversationAvatar({
  participants,
  currentId,
}: {
  participants: readonly Participant[];
  currentId: string;
}) {
  const peers = conversationPeers(participants, currentId)
    .slice()
    .sort((a, b) => a.id.localeCompare(b.id));
  if (peers.length <= 1)
    return (
      <IdentityIcon
        id={peers[0]?.id}
        kind={peers[0]?.kind || "conversation"}
        label={peers[0]?.name}
      />
    );
  return (
    <span
      className="conversation-avatar"
      data-count={Math.min(4, peers.length)}
      aria-hidden="true"
    >
      {peers.slice(0, peers.length > 4 ? 3 : 4).map((person) => (
        <IdentityIcon
          key={person.id}
          id={person.id}
          kind={person.kind}
          label={person.name}
        />
      ))}
      {peers.length > 4 ? (
        <span className="avatar-more">+{peers.length - 3}</span>
      ) : null}
    </span>
  );
}
export function RequestKind({
  kind,
  iconOnly = false,
}: {
  kind: string;
  iconOnly?: boolean;
}) {
  const glyph =
    kind === "approval" ? "permissions" : kind === "task" ? "task" : "question";
  return iconOnly ? (
    <span className={`request-kind-icon request-${kind}`} aria-hidden="true">
      <Icon name={glyph} />
    </span>
  ) : (
    <Badge tone={`request-${kind}`}>
      <Icon name={glyph} size={12} />
      {kind}
    </Badge>
  );
}
export function Button({
  children,
  primary = false,
  danger = false,
  icon,
  type = "button",
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & {
  primary?: boolean;
  danger?: boolean;
  icon?: string;
}) {
  return (
    <button
      {...props}
      type={type}
      className={`button ${primary ? "primary" : ""} ${danger ? "danger" : ""} ${props.className || ""}`}
    >
      {icon ? <Icon name={icon} size={15} /> : null}
      {children}
    </button>
  );
}
export function Badge({
  children,
  tone = "neutral",
}: {
  children: ReactNode;
  tone?: string;
}) {
  return <span className={`badge ${tone}`}>{children}</span>;
}
const requestStates: Record<string, { label: string; tone: string }> = {
  open: { label: "Awaiting reply", tone: "warn" },
  clarification: { label: "Needs context", tone: "accent" },
  resolved: { label: "Resolved", tone: "good" },
  answered: { label: "Answered", tone: "good" },
  approved: { label: "Approved", tone: "good" },
  completed: { label: "Completed", tone: "good" },
  denied: { label: "Declined", tone: "danger" },
  declined: { label: "Declined", tone: "danger" },
  rejected: { label: "Rejected", tone: "danger" },
  failed: { label: "Failed", tone: "danger" },
  expired: { label: "Expired", tone: "neutral" },
  cancelled: { label: "Cancelled", tone: "neutral" },
  superseded: { label: "Replaced", tone: "neutral" },
};
export function RequestStatus({
  request,
}: {
  request: Pick<Request, "state" | "response">;
}) {
  const value =
    request.state === "resolved"
      ? request.response?.outcome || request.state
      : request.state;
  const status = requestStates[value];
  return (
    <Badge tone={status?.tone}>
      {status?.label || value.replaceAll("_", " ")}
    </Badge>
  );
}
// The conversation API uses exact outcome labels as its latest preview.
// Only style complete labels; never infer a state from message substrings.
export function ConversationPreview({ body }: { body: string }) {
  const status = [
    "answered",
    "approved",
    "denied",
    "completed",
    "failed",
    "rejected",
  ].includes(body)
    ? requestStates[body]
    : undefined;
  return status ? (
    <Badge tone={status.tone}>{status.label}</Badge>
  ) : (
    <>{body}</>
  );
}
export function Status({ value }: { value: string }) {
  const good = [
    "connected",
    "online",
    "received",
    "succeeded",
    "completed",
    "approved",
  ];
  const warn = ["stale", "unknown", "waiting"];
  return (
    <span className="status">
      <i
        className={
          good.includes(value) ? "good" : warn.includes(value) ? "warn" : ""
        }
      />
      {value.replaceAll("_", " ")}
    </span>
  );
}
export function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <label className="field">
      <span>{label}</span>
      {children}
      {hint ? <small>{hint}</small> : null}
    </label>
  );
}
export function ErrorView({
  error,
  retry,
}: {
  error: unknown;
  retry?: () => void;
}) {
  if (!error) return null;
  const e = error as Error;
  return (
    <div className="error" role="alert">
      <Icon name="activity" size={16} />
      <span>{e.message || "This information is unavailable."}</span>
      {retry ? <button onClick={retry}>Try again</button> : null}
    </div>
  );
}
export function Empty({
  title = "Nothing here yet",
  children,
  icon = "inbox",
}: {
  title?: string;
  children?: ReactNode;
  icon?: string;
}) {
  return (
    <div className="empty">
      <Icon name={icon} size={27} />
      <h3>{title}</h3>
      {children ? <p>{children}</p> : null}
    </div>
  );
}
export function Loading({ label = "Loading" }: { label?: string }) {
  return (
    <div className="loading" role="status">
      <span className="spinner" />
      {label}
    </div>
  );
}
export function PageHeader({
  title,
  description,
  actions,
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
}) {
  return (
    <header className="page-header">
      <div>
        <h1>{title}</h1>
        {description ? <p>{description}</p> : null}
      </div>
      {actions ? <div className="actions">{actions}</div> : null}
    </header>
  );
}
export function Search({
  value,
  onChange,
  placeholder = "Search",
}: {
  value: string;
  onChange: (v: string) => void;
  placeholder?: string;
}) {
  return (
    <div className="search">
      <Icon name="search" size={16} />
      <input
        type="search"
        aria-label={placeholder}
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    </div>
  );
}
export function Tabs({
  value,
  onChange,
  options,
}: {
  value: string;
  onChange: (v: string) => void;
  options: { id: string; label: string }[];
}) {
  return (
    <div className="tabs" role="tablist">
      {options.map((o) => (
        <button
          key={o.id}
          role="tab"
          aria-selected={value === o.id}
          tabIndex={value === o.id ? 0 : -1}
          onKeyDown={(e) => {
            const index = options.findIndex((x) => x.id === o.id);
            const next =
              e.key === "ArrowRight"
                ? (index + 1) % options.length
                : e.key === "ArrowLeft"
                  ? (index - 1 + options.length) % options.length
                  : e.key === "Home"
                    ? 0
                    : e.key === "End"
                      ? options.length - 1
                      : -1;
            if (next >= 0) {
              e.preventDefault();
              onChange(options[next].id);
              const buttons =
                e.currentTarget.parentElement?.querySelectorAll<HTMLButtonElement>(
                  "[role=tab]",
                );
              buttons?.[next]?.focus();
            }
          }}
          onClick={() => onChange(o.id)}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}
export function Modal({
  title,
  children,
  onClose,
  wide = false,
  className = "",
}: {
  title: string;
  children: ReactNode;
  onClose: () => void;
  wide?: boolean;
  className?: string;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  useEffect(() => {
    const d = ref.current!;
    const previous = document.activeElement as HTMLElement | null;
    d.showModal();
    return () => {
      d.close();
      if (previous?.isConnected) previous.focus();
    };
  }, []);
  return (
    <dialog
      ref={ref}
      className={`${wide ? "wide" : ""} ${className}`}
      onCancel={(e) => {
        e.preventDefault();
        e.stopPropagation();
        onClose();
      }}
      onClick={(e) => {
        if (e.target === ref.current) onClose();
      }}
      aria-labelledby={titleId}
    >
      <header>
        <h2 id={titleId}>{title}</h2>
        <button className="icon-button" onClick={onClose} aria-label="Close">
          <Icon name="close" />
        </button>
      </header>
      <div className="dialog-content">{children}</div>
    </dialog>
  );
}
export function Details({
  value,
  label = "Technical details",
  defer = false,
}: {
  value: unknown;
  label?: string;
  defer?: boolean;
}) {
  const [open, setOpen] = useState(false);
  return (
    <details
      className="details"
      onToggle={(e) => setOpen(e.currentTarget.open)}
    >
      <summary>{label}</summary>
      {!defer || open ? <pre>{readable(value)}</pre> : null}
    </details>
  );
}
export function DateValue({ value }: { value?: string }) {
  return (
    <time dateTime={value} title={value}>
      {timestamp(value)}
    </time>
  );
}
export function Form({
  onSubmit,
  children,
}: {
  onSubmit: (e: FormEvent<HTMLFormElement>) => void;
  children: ReactNode;
}) {
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit(e);
      }}
      className="form"
    >
      {children}
    </form>
  );
}
export function JsonField({
  label,
  value,
  onChange,
  rows = 5,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  rows?: number;
}) {
  return (
    <Field label={label}>
      <textarea
        className="code-input"
        rows={rows}
        spellCheck={false}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    </Field>
  );
}
export function Copy({
  value,
  label = "Copy",
}: {
  value: string;
  label?: string;
}) {
  const [copied, setCopied] = useState(false);
  return (
    <Button
      icon={copied ? "check" : "copy"}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(value);
          setCopied(true);
          setTimeout(() => setCopied(false), 1800);
        } catch {
          setCopied(false);
        }
      }}
    >
      {copied ? "Copied" : label}
    </Button>
  );
}
export function QueryState({
  query,
  children,
  empty = "No matching records",
}: {
  query: {
    isPending: boolean;
    error: unknown;
    items?: unknown[];
    refetch: () => unknown;
    hasNextPage?: boolean;
    isFetchingNextPage?: boolean;
    fetchNextPage?: () => unknown;
  };
  children: ReactNode;
  empty?: string;
}) {
  if (query.isPending) return <Loading />;
  if (query.error && (!query.items?.length || unavailable(query.error)))
    return <ErrorView error={query.error} retry={() => void query.refetch()} />;
  return (
    <>
      {query.items?.length === 0 ? <Empty title={empty} /> : children}
      {query.hasNextPage ? (
        <div className="load-more">
          <Button
            disabled={query.isFetchingNextPage}
            onClick={() => void query.fetchNextPage?.()}
          >
            {query.isFetchingNextPage ? "Loading" : "Load more"}
          </Button>
        </div>
      ) : null}
    </>
  );
}
export function unavailable(error: unknown) {
  return error instanceof APIError && [403, 404].includes(error.status);
}
