// Original vector marks. They identify records visually, not agent capabilities.
const marks = [
  <g>
    <path d="M16 3 21 11 16 16 11 11Z" />
    <path d="m29 16-8 5-5-5 5-5Z" opacity=".68" />
    <path d="m16 29-5-8 5-5 5 5Z" />
    <path d="m3 16 8-5 5 5-5 5Z" opacity=".68" />
  </g>,
  <g fill="none" stroke="currentColor" strokeWidth="2.3">
    <ellipse cx="16" cy="16" rx="13" ry="6" transform="rotate(-38 16 16)" />
    <ellipse
      cx="16"
      cy="16"
      rx="6"
      ry="13"
      transform="rotate(-38 16 16)"
      opacity=".65"
    />
    <circle cx="16" cy="16" r="2.8" fill="currentColor" stroke="none" />
  </g>,
  <g>
    <path d="M5 9 16 3 27 9 16 15Z" />
    <path d="m5 12 9 5v12l-9-5Z" opacity=".62" />
    <path d="m18 17 9-5v12l-9 5Z" opacity=".9" />
  </g>,
  <g>
    {[0, 60, 120, 180, 240, 300].map((angle) => (
      <path
        key={angle}
        d="m16 3 11 7-11 4Z"
        transform={`rotate(${angle} 16 16)`}
        opacity={angle % 120 === 0 ? 1 : 0.62}
      />
    ))}
  </g>,
  <g fill="none" stroke="currentColor" strokeWidth="4" strokeLinecap="round">
    <path d="M6 11 16 5 26 11" />
    <path d="m6 17 10-6 10 6" opacity=".72" />
    <path d="m6 23 10-6 10 6" opacity=".44" />
  </g>,
  <g>
    <path d="M4 6h9v9H4zM19 17h9v9h-9z" />
    <path d="M19 6h9v9h-9zM4 17h9v9H4z" opacity=".5" />
    <path d="m13 13 6 6m-6 0 6-6" stroke="currentColor" strokeWidth="2.5" />
  </g>,
  <g fill="none" stroke="currentColor" strokeWidth="3.4" strokeLinecap="round">
    <path d="M6 9h12a7 7 0 0 1 0 14h-3" />
    <path d="M26 23H14a7 7 0 0 1 0-14h3" opacity=".58" />
  </g>,
  <g>
    <path d="M4 22 11 7h6l-7 15Z" opacity=".55" />
    <path d="m12 25 8-18h7l-8 18Z" />
    <circle cx="26" cy="25" r="2.5" opacity=".7" />
  </g>,
  <g
    fill="none"
    stroke="currentColor"
    strokeWidth="2.5"
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    <path d="M7 7v12l9 6 9-6V7" opacity=".6" />
    <path d="m7 7 9 6 9-6M16 13v12" />
    <circle cx="7" cy="7" r="3" fill="currentColor" stroke="none" />
    <circle cx="25" cy="7" r="3" fill="currentColor" stroke="none" />
    <circle cx="16" cy="25" r="3" fill="currentColor" stroke="none" />
  </g>,
  <g>
    <path d="M16 3 20 12 29 16 20 20 16 29 12 20 3 16 12 12Z" />
    <circle cx="16" cy="16" r="3" fill="var(--agent-face)" />
  </g>,
  <g fill="none" stroke="currentColor" strokeWidth="3.5" strokeLinecap="round">
    <path d="M7 24V13a9 9 0 0 1 18 0v11" />
    <path d="M12 24V13a4 4 0 0 1 8 0v11" opacity=".55" />
  </g>,
  <g>
    <path d="M4 5h10v10H4zM18 17h10v10H18z" />
    <path d="M18 5h10v10H18zM4 17h10v10H4z" opacity=".42" />
    <circle cx="16" cy="16" r="3.4" />
  </g>,
];

export function identitySeed(key: string) {
  let hash = 2166136261;
  for (const char of key)
    hash = Math.imul(hash ^ char.codePointAt(0)!, 16777619);
  return hash >>> 0;
}

export function AgentMark({ seed }: { seed: number }) {
  const index = (seed >>> 8) % marks.length;
  return (
    <svg
      className="agent-mark"
      data-mark={index}
      viewBox="0 0 32 32"
      fill="currentColor"
      aria-hidden="true"
    >
      <use
        className="mark-outline"
        width="32"
        height="32"
        href={`/agent-icons/marks.svg#lucide-${index}`}
      />
      <use
        className="mark-duotone"
        width="32"
        height="32"
        href={`/agent-icons/marks.svg#phosphor-${index}`}
      />
      <g className="mark-geometry" fill="currentColor">
        {marks[index]}
      </g>
    </svg>
  );
}
