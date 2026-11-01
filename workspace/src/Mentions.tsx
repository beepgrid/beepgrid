import {
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
} from "react";
import { createPortal } from "react-dom";
import type { Identity, Mention } from "./contracts";
import { IdentityIcon } from "./ui";
import { workspaceURL } from "./navigation";

// Preserve stable addressing when unrelated text changes, and discard a token
// when its own text is edited. Offsets count Unicode code points, not UTF-16.
export function editedMentions(
  before: string,
  after: string,
  mentions: Mention[],
) {
  const a = Array.from(before),
    b = Array.from(after);
  let start = 0,
    endA = a.length,
    endB = b.length;
  while (start < endA && start < endB && a[start] === b[start]) start++;
  while (endA > start && endB > start && a[endA - 1] === b[endB - 1]) {
    endA--;
    endB--;
  }
  return mentions.flatMap((m) =>
    m.end <= start
      ? [m]
      : m.start >= endA
        ? [{ ...m, start: m.start + endB - endA, end: m.end + endB - endA }]
        : [],
  );
}
export function MentionText({
  body,
  mentions = [],
  participants = [],
  links = true,
}: {
  body: string;
  mentions?: Mention[];
  participants?: Identity[];
  links?: boolean;
}) {
  const runes = Array.from(body),
    pieces = [];
  let last = 0;
  for (const [i, m] of mentions.entries()) {
    if (m.start < last || m.end > runes.length || m.end <= m.start) continue;
    pieces.push(runes.slice(last, m.start).join(""));
    const p = participants.find((p) => p.id === m.principal_id);
    const text = runes.slice(m.start, m.end).join("");
    pieces.push(
      p && p.kind !== "service" && links ? (
        <a
          className="mention-token"
          href={workspaceURL(
            `/${p.kind === "person" ? "people" : "agents"}/${encodeURIComponent(p.id)}`,
          )}
          key={i}
          title={p.name}
        >
          {text}
        </a>
      ) : (
        <span className="mention-token" key={i}>
          {text}
        </span>
      ),
    );
    last = m.end;
  }
  pieces.push(runes.slice(last).join(""));
  return <>{pieces}</>;
}
export function MentionEditor({
  value,
  onChange,
  mentions,
  onMentions,
  participants = [],
  rows = 3,
  label = "Message",
  placeholder,
  disabled = false,
  onKeyDown,
}: {
  value: string;
  onChange: (v: string) => void;
  mentions: Mention[];
  onMentions: (v: Mention[]) => void;
  participants?: Identity[];
  rows?: number;
  label?: string;
  placeholder?: string;
  disabled?: boolean;
  onKeyDown: (e: KeyboardEvent<HTMLTextAreaElement>) => void;
}) {
  const ref = useRef<HTMLTextAreaElement>(null);
  const listID = useId();
  const [position, setPosition] = useState<{
    left: number;
    top: number;
    width: number;
    maxHeight: number;
  } | null>(null);
  const [anchor, setAnchor] = useState<{
      start: number;
      end: number;
      term: string;
    } | null>(null),
    [active, setActive] = useState(0),
    [scroll, setScroll] = useState(0);
  const candidates = anchor
    ? participants
        .filter((p) => p.name.toLowerCase().includes(anchor.term.toLowerCase()))
        .slice(0, 6)
    : [];
  useLayoutEffect(() => {
    if (!candidates.length) {
      setPosition(null);
      return;
    }
    const place = () => {
      if (!ref.current) return;
      const rect = ref.current.getBoundingClientRect();
      const desired = candidates.length * 44 + 12;
      const above = rect.top - 14,
        below = innerHeight - rect.bottom - 14;
      const upward = above >= desired || above >= below;
      const maxHeight = Math.max(44, Math.min(desired, upward ? above : below));
      const width = Math.min(340, Math.max(260, rect.width), innerWidth - 16);
      setPosition({
        left: Math.max(8, Math.min(rect.left, innerWidth - width - 8)),
        top: upward ? Math.max(8, rect.top - maxHeight - 6) : rect.bottom + 6,
        width,
        maxHeight,
      });
    };
    place();
    window.addEventListener("resize", place);
    window.addEventListener("scroll", place, true);
    return () => {
      window.removeEventListener("resize", place);
      window.removeEventListener("scroll", place, true);
    };
  }, [candidates.length, anchor?.start, anchor?.term]);
  function locate(text: string, cursor: number) {
    const prefix = text.slice(0, cursor);
    const match = prefix.match(/(?:^|\s)@([^@\n]{0,60})$/);
    const next = match
      ? { start: cursor - match[1].length - 1, end: cursor, term: match[1] }
      : null;
    if (
      next?.start !== anchor?.start ||
      next?.term !== anchor?.term ||
      next?.end !== anchor?.end
    )
      setActive(0);
    setAnchor(next);
  }
  function choose(p: Identity) {
    if (!anchor) return;
    const text = "@" + p.name;
    const next =
      value.slice(0, anchor.start) + text + " " + value.slice(anchor.end);
    const shifted = editedMentions(value, next, mentions);
    const start = Array.from(value.slice(0, anchor.start)).length;
    onChange(next);
    onMentions(
      [
        ...shifted,
        { principal_id: p.id, start, end: start + Array.from(text).length },
      ].sort((a, b) => a.start - b.start),
    );
    setAnchor(null);
    requestAnimationFrame(() => {
      ref.current?.focus();
      ref.current?.setSelectionRange(
        anchor.start + text.length + 1,
        anchor.start + text.length + 1,
      );
    });
  }
  return (
    <div className="mention-editor">
      {mentions.length > 0 ? (
        <div className="mention-mirror" aria-hidden="true">
          <div style={{ transform: `translateY(-${scroll}px)` }}>
            <MentionText body={value} mentions={mentions} links={false} />
            {"\n"}
          </div>
        </div>
      ) : null}
      <textarea
        disabled={disabled}
        ref={ref}
        className={`message-input ${mentions.length ? "has-mentions" : ""}`}
        aria-label={label}
        aria-autocomplete={participants.length > 2 ? "list" : undefined}
        aria-controls={candidates.length ? listID : undefined}
        aria-activedescendant={
          candidates.length ? `${listID}-option-${active}` : undefined
        }
        aria-expanded={candidates.length ? true : undefined}
        placeholder={
          placeholder ||
          (participants.length > 2
            ? "Write a message. Use @ to mention someone..."
            : "Write a message...")
        }
        rows={rows}
        value={value}
        onChange={(e) => {
          onMentions(editedMentions(value, e.target.value, mentions));
          onChange(e.target.value);
          if (participants.length > 2)
            locate(e.target.value, e.target.selectionStart);
        }}
        onSelect={(e) => {
          if (participants.length > 2)
            locate(value, e.currentTarget.selectionStart);
        }}
        onScroll={(e) => setScroll(e.currentTarget.scrollTop)}
        onBlur={() => setAnchor(null)}
        onKeyDown={(e) => {
          if (e.nativeEvent.isComposing || e.nativeEvent.keyCode === 229)
            return;
          if (candidates.length) {
            if (e.key === "ArrowDown" || e.key === "ArrowUp") {
              e.preventDefault();
              setActive(
                (a) =>
                  (a + (e.key === "ArrowDown" ? 1 : candidates.length - 1)) %
                  candidates.length,
              );
              return;
            }
            if (
              e.key === "Enter" &&
              !e.shiftKey &&
              !e.altKey &&
              !e.metaKey &&
              !e.ctrlKey &&
              !e.repeat
            ) {
              e.preventDefault();
              choose(candidates[active]);
              return;
            }
            if (e.key === "Escape") {
              e.preventDefault();
              e.stopPropagation();
              setAnchor(null);
              return;
            }
          }
          onKeyDown(e);
        }}
      />
      {candidates.length && position
        ? createPortal(
            <div
              className="mention-suggestions"
              id={listID}
              role="listbox"
              aria-label="Mention a participant"
              style={position}
            >
              {candidates.map((p, i) => (
                <button
                  type="button"
                  id={`${listID}-option-${i}`}
                  role="option"
                  aria-selected={active === i}
                  key={p.id}
                  onPointerDown={(e) => e.preventDefault()}
                  onMouseDown={(e) => e.preventDefault()}
                  onClick={() => choose(p)}
                >
                  <IdentityIcon
                    kind={p.kind}
                    id={p.id}
                    label={p.name}
                    icon={String(p.metadata?.appearance_icon || "")}
                  />
                  <strong>{p.name}</strong>
                  <small>
                    {p.kind} · {p.id.slice(-4)}
                  </small>
                </button>
              ))}
            </div>,
            ref.current?.closest("dialog") || document.body,
          )
        : null}
    </div>
  );
}
