import { useResource } from "./data";
import type { AssistanceSettings } from "./contracts";
import { navigate } from "./navigation";
import { Badge } from "./ui";

export const assistantName = "Coordinator";

export function OverseerMark() {
  return (
    <span className="identity-icon overseer-mark" aria-hidden="true">
      <svg
        width="30"
        height="30"
        viewBox="0 0 36 36"
        fill="none"
        aria-hidden="true"
      >
        <path
          d="M8.5 18H13m7-2 7-8m-7 10h7m-7 2 7 8"
          stroke="currentColor"
          strokeWidth="1.6"
          strokeLinecap="round"
        />
        <circle
          cx="5.5"
          cy="18"
          r="3"
          stroke="currentColor"
          strokeWidth="1.6"
        />
        <circle
          cx="30"
          cy="6.5"
          r="3"
          stroke="currentColor"
          strokeWidth="1.6"
        />
        <circle cx="30" cy="18" r="3" stroke="currentColor" strokeWidth="1.6" />
        <circle
          cx="30"
          cy="29.5"
          r="3"
          stroke="currentColor"
          strokeWidth="1.6"
        />
        <circle cx="17" cy="18" r="4.5" fill="var(--accent)" />
      </svg>
    </span>
  );
}
export function OverseerEntry({ selected = false }: { selected?: boolean }) {
  const settings = useResource<AssistanceSettings>("/assistance/settings");
  const state = settings.error
    ? "Unavailable"
    : settings.data?.overseer
      ? "On"
      : settings.data
        ? settings.data.overseer_configured
          ? "Off"
          : "Not set up"
        : "Checking";
  return (
    <button
      className="conversation-row conversation-preview overseer-entry"
      aria-current={selected}
      onClick={() => navigate("/conversations/overseer")}
    >
      <OverseerMark />
      <div className="conversation-copy">
        <div className="coordinator-entry-title">
          <strong>{assistantName}</strong>
        </div>
        <p>
          <Badge tone="accent">Built-in Agent</Badge>
        </p>
      </div>
      <span className="badge coordinator-state" data-state={state}>
        {state}
      </span>
    </button>
  );
}
