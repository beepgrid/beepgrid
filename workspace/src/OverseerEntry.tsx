import { useResource } from "./data";
import type { AssistanceSettings } from "./contracts";
import { navigate } from "./navigation";
import { Badge, Icon } from "./ui";

export const assistantName = "Coordinator";

export function OverseerMark() {
  return (
    <span className="identity-icon overseer-mark" aria-hidden="true">
      <svg
        width="27"
        height="25"
        style={{ width: 27, height: 25 }}
        viewBox="0 0 28 26"
        fill="none"
        aria-hidden="true"
      >
        <path
          d="M6 13h5m5-1 6-7m-6 8h6m-6 1 6 7"
          stroke="currentColor"
          strokeWidth="1.35"
          strokeLinecap="round"
        />
        <circle
          cx="3.5"
          cy="13"
          r="2.5"
          stroke="currentColor"
          strokeWidth="1.35"
        />
        <circle
          cx="24"
          cy="4"
          r="2.2"
          stroke="currentColor"
          strokeWidth="1.35"
        />
        <circle
          cx="24"
          cy="13"
          r="2.2"
          stroke="currentColor"
          strokeWidth="1.35"
        />
        <circle
          cx="24"
          cy="22"
          r="2.2"
          stroke="currentColor"
          strokeWidth="1.35"
        />
        <circle cx="13.5" cy="13" r="3.5" fill="var(--accent)" />
      </svg>
    </span>
  );
}
export function OverseerEntry({ selected = false }: { selected?: boolean }) {
  const settings = useResource<AssistanceSettings>("/assistance/settings");
  return (
    <button
      className="conversation-row conversation-preview overseer-entry"
      aria-current={selected}
      onClick={() => navigate("/conversations/overseer")}
    >
      <OverseerMark />
      <div className="conversation-copy">
        <strong>{assistantName}</strong>
        <p>Your private agent coordinator</p>
        <div className="conversation-meta">
          <span>Built-in assistant</span>
          <Badge>
            {settings.data?.overseer
              ? "On"
              : settings.data
                ? "Off"
                : "Checking"}
          </Badge>
        </div>
      </div>
      <Icon name="chevron" size={16} />
    </button>
  );
}
