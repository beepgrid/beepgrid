import { useEffect, useLayoutEffect, useRef, useState } from "react";
import presets from "./icon-presets.json";
import { Icon, IdentityIcon } from "./ui";

export const iconKey = "beepgrid:workspace:agent-icons:v1";
export function savedIcons() {
  try {
    return (
      presets.find((p) => p.id === localStorage.getItem(iconKey)) || presets[0]
    );
  } catch {
    return presets[0];
  }
}
export function IconPicker({
  open,
  onOpen,
  onClose,
}: {
  open: boolean;
  onOpen: () => void;
  onClose: () => void;
}) {
  const [preset, setPreset] = useState(savedIcons);
  const [saved, setSaved] = useState(true);
  const trigger = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLElement>(null);
  const index = presets.findIndex((p) => p.id === preset.id);
  useLayoutEffect(() => {
    document.documentElement.dataset.agentIcons = preset.id;
  }, [preset.id]);
  useEffect(() => {
    try {
      localStorage.setItem(iconKey, preset.id);
      setSaved(true);
    } catch {
      setSaved(false);
    }
  }, [preset.id]);
  useEffect(() => {
    if (!open) return;
    panel.current?.querySelector<HTMLSelectElement>("select")?.focus();
    const escape = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        onClose();
        trigger.current?.focus();
      }
    };
    const outside = (e: PointerEvent) => {
      if (
        !panel.current?.contains(e.target as Node) &&
        !trigger.current?.contains(e.target as Node)
      )
        onClose();
    };
    document.addEventListener("keydown", escape);
    document.addEventListener("pointerdown", outside);
    return () => {
      document.removeEventListener("keydown", escape);
      document.removeEventListener("pointerdown", outside);
    };
  }, [open, onClose]);
  function choose(id: string) {
    setPreset(presets.find((p) => p.id === id) || presets[0]);
  }
  return (
    <>
      <button
        ref={trigger}
        type="button"
        className="type-trigger"
        aria-label="Open Agent icon picker"
        title="Agent icon styles"
        aria-expanded={open}
        aria-controls="agent-icon-panel"
        onClick={open ? onClose : onOpen}
      >
        <Icon name="agents" size={19} />
      </button>
      {open ? (
        <section
          ref={panel}
          id="agent-icon-panel"
          className="type-panel"
          aria-label="Agent icon picker"
        >
          <header>
            <h2>Agent icons</h2>
            <button
              type="button"
              className="type-icon"
              aria-label="Close Agent icon picker"
              onClick={() => {
                onClose();
                trigger.current?.focus();
              }}
            >
              <Icon name="close" size={17} />
            </button>
          </header>
          <p className="type-intro">Preview across Agents and conversations.</p>
          <label className="type-label" htmlFor="agent-icon-preset">
            Icon pack & surface
          </label>
          <select
            id="agent-icon-preset"
            value={preset.id}
            onChange={(e) => choose(e.target.value)}
          >
            {presets.map((p, i) => (
              <option key={p.id} value={p.id}>
                {String(i + 1).padStart(2, "0")} · {p.name}
              </option>
            ))}
          </select>
          <div className="type-step">
            <button
              type="button"
              disabled={index === 0}
              onClick={() => choose(presets[index - 1].id)}
            >
              ← Previous
            </button>
            <span>
              {index + 1} / {presets.length}
            </span>
            <button
              type="button"
              disabled={index === presets.length - 1}
              onClick={() => choose(presets[index + 1].id)}
            >
              Next →
            </button>
          </div>
          <p className="type-description">{preset.description}</p>
          <div className="icon-samples" aria-label="Example Agent icons">
            {["scout", "builder", "reviewer", "sentinel", "triage"].map(
              (name) => (
                <IdentityIcon key={name} id={name} label={name} />
              ),
            )}
          </div>
          <div className="type-compare">
            <button type="button" onClick={() => choose(presets[0].id)}>
              Reset to Outline
            </button>
          </div>
          <footer>
            <span role="status">{preset.name}</span>
            <small>
              {saved
                ? "Saved on this browser"
                : "Preview only: browser storage unavailable"}
            </small>
            <small>
              Visual identities only. Roles and access stay unchanged.
            </small>
          </footer>
        </section>
      ) : null}
    </>
  );
}
