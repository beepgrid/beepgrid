import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { Capability, Identity, Request } from "./contracts";
import { bytes } from "./client";
import {
  accessUnavailable,
  useDraft,
  useOperation,
  usePostResource,
} from "./data";
import { Button, ErrorView, Form, Icon, IdentityIcon, Loading } from "./ui";

// This lives outside the scrolling timeline. Chat text never resolves a request;
// only the answer form for this exact request ID/version may do that.
export function PendingQuestions({
  questions,
  participants,
  hasMore,
  loadingMore,
  onMore,
}: {
  questions: Request[];
  participants: Identity[];
  hasMore: boolean;
  loadingMore: boolean;
  onMore: () => void;
}) {
  const [selected, select] = useState<string | null>(null);
  const [collapsed, collapse] = useState(false);
  const [forwardFrom, setForwardFrom] = useState<string | null>(null);
  const sorted = [...questions].sort((a, b) =>
    b.created_at.localeCompare(a.created_at),
  );
  const index = Math.max(
    0,
    sorted.findIndex((q) => q.id === selected),
  );
  const current = sorted[index];
  // Keep the question being read or drafted stable when live arrivals reorder
  // the list. A resolved/cancelled question naturally advances to the newest.
  useEffect(() => {
    if (current && current.id !== selected) select(current.id);
  }, [current?.id, selected]);
  useEffect(() => {
    const from = sorted.findIndex((q) => q.id === forwardFrom);
    if (from >= 0 && from < sorted.length - 1) {
      select(sorted[from + 1].id);
      setForwardFrom(null);
    }
  }, [questions, forwardFrom]);
  if (!current) return null;
  const author = participants.find((p) => p.id === current.requester_id);
  return (
    <section className="question-dock" aria-label="Pending questions">
      <header className="question-dock-head">
        <IdentityIcon
          kind={author?.kind || "agent"}
          id={current.requester_id}
          label={author?.name}
          icon={String(author?.metadata?.appearance_icon || "")}
        />
        <button
          className="question-heading"
          type="button"
          aria-expanded={!collapsed}
          aria-controls="pending-question-answer"
          onClick={() => collapse(!collapsed)}
        >
          <strong>
            {collapsed
              ? current.body || current.title
              : `Question from ${author?.name || "Participant"}`}
          </strong>
        </button>
        <nav aria-label="Question navigation">
          <span aria-live="polite">
            {index + 1} / {sorted.length}
            {hasMore ? "+" : ""}
          </span>
          <button
            className="icon-button question-previous"
            type="button"
            aria-label="Previous question"
            disabled={index === 0}
            onClick={() => select(sorted[index - 1].id)}
          >
            <Icon name="chevron" size={15} />
          </button>
          <button
            className="icon-button"
            type="button"
            aria-label="Next question"
            disabled={index === sorted.length - 1 && (!hasMore || loadingMore)}
            onClick={() =>
              index < sorted.length - 1
                ? select(sorted[index + 1].id)
                : (setForwardFrom(current.id), onMore())
            }
          >
            <Icon name="chevron" size={15} />
          </button>
        </nav>
        <button
          className="icon-button question-toggle"
          type="button"
          aria-label={collapsed ? "Expand questions" : "Minimize questions"}
          aria-expanded={!collapsed}
          onClick={() => collapse(!collapsed)}
        >
          <Icon name="chevron" size={15} />
        </button>
      </header>
      <div id="pending-question-answer" hidden={collapsed}>
        <QuestionAnswer
          key={current.id}
          request={current}
          expanded={!collapsed}
        />
      </div>
    </section>
  );
}

export function QuestionAnswer({
  request: r,
  expanded = true,
}: {
  request: Request;
  expanded?: boolean;
}) {
  const [answer, setAnswer] = useDraft(`reply:${r.conversation_id}:${r.id}`);
  const op = useOperation(`reply:${r.conversation_id}:${r.id}`);
  const caps = usePostResource<Capability>("/capabilities", {
    target: {},
    request_id: r.id,
  });
  const input = useRef<HTMLTextAreaElement>(null);
  useLayoutEffect(() => {
    if (input.current) {
      input.current.style.height = "auto";
      input.current.style.height =
        Math.min(input.current.scrollHeight, 96) + "px";
    }
  }, [answer, expanded, caps.data]);
  useEffect(() => {
    if (
      (caps.data && !caps.data.operations.respond) ||
      accessUnavailable(caps.error)
    ) {
      setAnswer("");
      op.forget();
    }
  }, [caps.data, caps.error]);
  async function submit() {
    if (
      !caps.data?.operations.respond ||
      op.pending ||
      !answer.trim() ||
      bytes(answer) > 32768 ||
      (r.choices?.length && !r.choices.includes(answer))
    )
      return;
    const result = await op.run(`/requests/${r.id}/responses`, {
      version: r.version,
      outcome: "answered",
      answer,
    });
    if (result) setAnswer("");
  }
  return (
    <div className="question-dock-body">
      <p className="question-prompt">{r.body || r.title}</p>
      {caps.isPending ? (
        <Loading label="Checking answer access" />
      ) : caps.error ? (
        <ErrorView error={caps.error} retry={() => void caps.refetch()} />
      ) : !caps.data?.operations.respond ? (
        <p>You can no longer answer this question.</p>
      ) : (
        <Form onSubmit={() => void submit()}>
          <ErrorView error={op.error} />
          {r.choices?.length ? (
            <fieldset className="question-options" aria-label="Answer choices">
              {r.choices.map((choice) => (
                <label key={choice}>
                  <input
                    type="radio"
                    name={r.id}
                    checked={answer === choice}
                    disabled={op.pending}
                    onChange={() => setAnswer(choice)}
                  />
                  <span>{choice}</span>
                </label>
              ))}
            </fieldset>
          ) : (
            <textarea
              ref={input}
              aria-label="Answer question"
              placeholder="Your answer..."
              value={answer}
              rows={1}
              disabled={op.pending}
              onChange={(e) => setAnswer(e.target.value)}
              onKeyDown={(e) => {
                if (
                  e.key === "Enter" &&
                  !e.shiftKey &&
                  !e.altKey &&
                  !e.nativeEvent.isComposing &&
                  e.nativeEvent.keyCode !== 229 &&
                  !e.repeat
                ) {
                  e.preventDefault();
                  void submit();
                }
              }}
            />
          )}
          <Button
            primary
            type="submit"
            disabled={
              op.pending ||
              !answer.trim() ||
              bytes(answer) > 32768 ||
              !!(r.choices?.length && !r.choices.includes(answer))
            }
          >
            {op.pending ? "Answering..." : "Answer"}
          </Button>
        </Form>
      )}
    </div>
  );
}
