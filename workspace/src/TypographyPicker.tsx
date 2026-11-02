import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import presets from "./typography-presets.json";
import { Icon } from "./ui";
import { IconPicker } from "./IconPicker";

export const typographyPresets = presets;
export const typographyKey = "beepgrid:workspace:type-preset:v1";
export function savedTypography() {
  try {
    return (
      presets.find((p) => p.id === localStorage.getItem(typographyKey)) ||
      presets[0]
    );
  } catch {
    return presets[0];
  }
}
export function TypographyPicker() {
  const [preset, setPreset] = useState(savedTypography);
  const [tool, setTool] = useState<"type" | "icons" | null>(null);
  const open = tool === "type";
  const closeTool = useCallback(() => setTool(null), []);
  const [original, setOriginal] = useState(false);
  const [fontState, setFontState] = useState("Loading font");
  const [retry, setRetry] = useState(0);
  const [saved, setSaved] = useState(true);
  const trigger = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLElement>(null);
  const index = presets.findIndex((p) => p.id === preset.id);
  useLayoutEffect(() => {
    document.documentElement.dataset.typography = original
      ? "original"
      : preset.id;
  }, [preset.id, original]);
  useEffect(() => {
    document.documentElement.dataset.typographyTool = "enabled";
    return () => {
      delete document.documentElement.dataset.typographyTool;
    };
  }, []);
  useEffect(() => {
    try {
      localStorage.setItem(typographyKey, preset.id);
      setSaved(true);
    } catch {
      setSaved(false);
    }
  }, [preset.id]);
  useEffect(() => {
    if (!preset.font || original) {
      setFontState("Ready");
      return;
    }
    let active = true;
    setFontState("Loading font");
    if (!document.fonts?.load) {
      setFontState("Font check unavailable");
      return;
    }
    document.fonts
      .load(
        `${preset.weight} ${preset.body}px "${preset.family}"`,
        "Agents Scout 0123456789",
      )
      .then((faces) => {
        if (active) setFontState(faces.length ? "Ready" : "Font unavailable");
      })
      .catch(() => {
        if (active) setFontState("Font unavailable");
      });
    return () => {
      active = false;
    };
  }, [preset, original, retry]);
  useEffect(() => {
    if (!open) return;
    panel.current?.querySelector<HTMLSelectElement>("select")?.focus();
    const escape = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.preventDefault();
        setTool(null);
        trigger.current?.focus();
      }
    };
    const outside = (e: PointerEvent) => {
      if (
        !panel.current?.contains(e.target as Node) &&
        !trigger.current?.contains(e.target as Node)
      )
        setTool(null);
    };
    document.addEventListener("keydown", escape);
    document.addEventListener("pointerdown", outside);
    return () => {
      document.removeEventListener("keydown", escape);
      document.removeEventListener("pointerdown", outside);
    };
  }, [open]);
  function choose(id: string) {
    setOriginal(false);
    setPreset(presets.find((p) => p.id === id) || presets[0]);
  }
  return (
    <div className="typography-tool">
      <IconPicker
        open={tool === "icons"}
        onOpen={() => setTool("icons")}
        onClose={closeTool}
      />
      <button
        ref={trigger}
        type="button"
        className="type-trigger"
        aria-label="Open typography picker"
        title="Typography presets"
        aria-expanded={open}
        aria-controls="typography-panel"
        onClick={() => setTool(open ? null : "type")}
      >
        Aa
      </button>
      {open ? (
        <section
          ref={panel}
          id="typography-panel"
          className="type-panel"
          aria-label="Typography picker"
        >
          <header>
            <h2>Typography</h2>
            <button
              type="button"
              className="type-icon"
              aria-label="Close typography picker"
              onClick={() => {
                setTool(null);
                trigger.current?.focus();
              }}
            >
              <Icon name="close" size={17} />
            </button>
          </header>
          <p className="type-intro">Ranked by closeness to your reference.</p>
          <label className="type-label" htmlFor="typography-preset">
            Font, weight & size
          </label>
          <select
            id="typography-preset"
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
          <dl className="type-metrics">
            <div>
              <dt>Body</dt>
              <dd>
                {preset.body}px / {preset.weight}
              </dd>
            </div>
            <div>
              <dt>Names</dt>
              <dd>
                {preset.body + 1.5}px / {preset.strong}
              </dd>
            </div>
            <div>
              <dt>Labels</dt>
              <dd>{preset.body - 1}px</dd>
            </div>
          </dl>
          <div className="type-compare">
            <button
              type="button"
              aria-pressed={original}
              onClick={() => setOriginal((v) => !v)}
            >
              {original ? "Return to selected type" : "Compare original type"}
            </button>
            <button type="button" onClick={() => choose(presets[0].id)}>
              Reset
            </button>
          </div>
          <footer>
            <span role="status">
              {original ? "Original type preview" : fontState}
            </span>
            {fontState === "Font unavailable" && !original ? (
              <button type="button" onClick={() => setRetry((v) => v + 1)}>
                Retry font
              </button>
            ) : null}
            <small>
              {saved
                ? "Saved on this browser"
                : "Preview only: browser storage unavailable"}
            </small>
          </footer>
        </section>
      ) : null}
    </div>
  );
}
