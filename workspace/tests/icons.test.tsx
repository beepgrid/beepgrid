import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { TypographyPicker } from "../src/TypographyPicker";
import { iconKey, savedIcons } from "../src/IconPicker";

beforeEach(() => {
  localStorage.clear();
  Object.defineProperty(document, "fonts", {
    configurable: true,
    value: { load: vi.fn().mockResolvedValue([{}]) },
  });
});
afterEach(() => {
  localStorage.clear();
  delete document.documentElement.dataset.agentIcons;
  delete document.documentElement.dataset.typography;
  delete document.documentElement.dataset.typographyTool;
});
function open() {
  fireEvent.click(
    screen.getByRole("button", { name: "Open agent icon picker" }),
  );
}
it("persists icon choices independently of typography and theme, including across remount", () => {
  localStorage.setItem("beepgrid:workspace:theme", "light");
  localStorage.setItem("beepgrid:workspace:type-preset:v1", "inter-reference");
  const { unmount } = render(<TypographyPicker />);
  open();
  expect(
    screen.getByRole("combobox", { name: "Icon pack & surface" }),
  ).toHaveFocus();
  fireEvent.change(screen.getByRole("combobox"), {
    target: { value: "phosphor-duotone" },
  });
  expect(document.documentElement.dataset.agentIcons).toBe("phosphor-duotone");
  expect(localStorage.getItem(iconKey)).toBe("phosphor-duotone");
  expect(localStorage.getItem("beepgrid:workspace:theme")).toBe("light");
  expect(document.documentElement.dataset.typography).toBe("inter-reference");
  unmount();
  render(<TypographyPicker />);
  open();
  expect(screen.getByRole("combobox")).toHaveValue("phosphor-duotone");
});
it("keeps the two preview panels mutually exclusive and supports keyboard dismissal", () => {
  render(<TypographyPicker />);
  open();
  fireEvent.click(
    screen.getByRole("button", { name: "Open typography picker" }),
  );
  expect(
    screen.queryByRole("region", { name: "Agent icon picker" }),
  ).not.toBeInTheDocument();
  open();
  expect(
    screen.queryByRole("region", { name: "Typography picker" }),
  ).not.toBeInTheDocument();
  fireEvent.keyDown(document, { key: "Escape" });
  expect(
    screen.queryByRole("region", { name: "Agent icon picker" }),
  ).not.toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: "Open agent icon picker" }),
  ).toHaveFocus();
});
it("steps through all five distinct packs and resets to the reference style", () => {
  render(<TypographyPicker />);
  open();
  expect(screen.getByRole("button", { name: "← Previous" })).toBeDisabled();
  for (const id of [
    "lucide-tint",
    "phosphor-duotone",
    "geometric-flat",
    "dimensional",
  ]) {
    fireEvent.click(screen.getByRole("button", { name: "Next →" }));
    expect(document.documentElement.dataset.agentIcons).toBe(id);
  }
  expect(screen.getByRole("button", { name: "Next →" })).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "Reset to Open Tabs" }));
  expect(document.documentElement.dataset.agentIcons).toBe("open-tabs");
});
it("falls back from obsolete preferences and handles denied storage without breaking preview", () => {
  localStorage.setItem(iconKey, "removed");
  expect(savedIcons().id).toBe("open-tabs");
  const spy = vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw Error("Denied");
  });
  render(<TypographyPicker />);
  open();
  fireEvent.change(screen.getByRole("combobox"), {
    target: { value: "geometric-flat" },
  });
  expect(document.documentElement.dataset.agentIcons).toBe("geometric-flat");
  expect(
    screen.getByText("Preview only: browser storage unavailable"),
  ).toBeVisible();
  spy.mockRestore();
});
it("dismisses on an outside click without changing the saved choice", () => {
  render(<TypographyPicker />);
  open();
  fireEvent.change(screen.getByRole("combobox"), {
    target: { value: "lucide-tint" },
  });
  fireEvent.pointerDown(document.body);
  expect(
    screen.queryByRole("region", { name: "Agent icon picker" }),
  ).not.toBeInTheDocument();
  expect(localStorage.getItem(iconKey)).toBe("lucide-tint");
});
