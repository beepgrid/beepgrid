import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  TypographyPicker,
  savedTypography,
  typographyKey,
} from "../src/TypographyPicker";

const load = vi.fn();
beforeEach(() => {
  localStorage.clear();
  load.mockReset().mockResolvedValue([{}]);
  Object.defineProperty(document, "fonts", {
    configurable: true,
    value: { load },
  });
});
afterEach(() => {
  localStorage.clear();
  delete document.documentElement.dataset.typography;
  delete document.documentElement.dataset.typographyTool;
});
function open() {
  fireEvent.click(
    screen.getByRole("button", { name: "Open typography picker" }),
  );
}
it("applies and retains a ranked preset across remounts without touching other local preferences", async () => {
  localStorage.setItem("beepgrid:workspace:theme", "light");
  const { unmount } = render(<TypographyPicker />);
  open();
  fireEvent.change(
    screen.getByRole("combobox", { name: "Font, weight & size" }),
    { target: { value: "source-readable" } },
  );
  expect(document.documentElement.dataset.typography).toBe("source-readable");
  await screen.findByText("Ready");
  expect(load).toHaveBeenLastCalledWith(
    '470 16px "BG Source Sans 3"',
    expect.any(String),
  );
  expect(localStorage.getItem(typographyKey)).toBe("source-readable");
  expect(localStorage.getItem("beepgrid:workspace:theme")).toBe("light");
  unmount();
  render(<TypographyPicker />);
  open();
  expect(screen.getByRole("combobox")).toHaveValue("source-readable");
});
it("compares the old typography temporarily and resets to the closest reference preset", async () => {
  render(<TypographyPicker />);
  open();
  fireEvent.click(screen.getByRole("button", { name: "Next →" }));
  expect(document.documentElement.dataset.typography).toBe("inter-strong");
  fireEvent.click(
    screen.getByRole("button", { name: "Compare original type" }),
  );
  expect(document.documentElement.dataset.typography).toBe("original");
  expect(localStorage.getItem(typographyKey)).toBe("inter-strong");
  fireEvent.click(
    screen.getByRole("button", { name: "Return to selected type" }),
  );
  expect(document.documentElement.dataset.typography).toBe("inter-strong");
  fireEvent.click(screen.getByRole("button", { name: "Reset" }));
  expect(screen.getByRole("combobox")).toHaveValue("inter-reference");
  await screen.findByText("Ready");
});
it("supports Escape and returns focus to the floating trigger", () => {
  render(<TypographyPicker />);
  open();
  expect(screen.getByRole("combobox")).toHaveFocus();
  fireEvent.keyDown(document, { key: "Escape" });
  expect(
    screen.queryByRole("region", { name: "Typography picker" }),
  ).not.toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: "Open typography picker" }),
  ).toHaveFocus();
});
it("reports a missing real font and allows retry instead of claiming a fallback is the selected typeface", async () => {
  load.mockResolvedValue([]);
  render(<TypographyPicker />);
  open();
  await screen.findByText("Font unavailable");
  load.mockResolvedValue([{}]);
  fireEvent.click(screen.getByRole("button", { name: "Retry font" }));
  await screen.findByText("Ready");
});
it("ignores a late font result after a different preset was selected", async () => {
  let finish!: (faces: unknown[]) => void;
  load.mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  render(<TypographyPicker />);
  open();
  fireEvent.change(screen.getByRole("combobox"), {
    target: { value: "system-reference" },
  });
  expect(screen.getByRole("status")).toHaveTextContent("Ready");
  await act(async () => finish([]));
  expect(screen.getByRole("status")).toHaveTextContent("Ready");
  expect(document.documentElement.dataset.typography).toBe("system-reference");
});
it("uses a safe default for obsolete preferences and keeps working when storage is unavailable", async () => {
  localStorage.setItem(typographyKey, "not-a-preset");
  expect(savedTypography().id).toBe("inter-reference");
  const storage = vi
    .spyOn(Storage.prototype, "setItem")
    .mockImplementation(() => {
      throw Error("Unavailable");
    });
  render(<TypographyPicker />);
  open();
  fireEvent.click(screen.getByRole("button", { name: "Next →" }));
  expect(document.documentElement.dataset.typography).toBe("inter-strong");
  await waitFor(() =>
    expect(
      screen.getByText("Preview only: browser storage unavailable"),
    ).toBeVisible(),
  );
  storage.mockRestore();
});
