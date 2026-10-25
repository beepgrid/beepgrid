import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Button, Modal, QueryState } from "../src/ui";
describe("shared primitives", () => {
  it("returns keyboard focus to the opener after a dialog closes", () => {
    function Example() {
      const [open, setOpen] = useState(false);
      return (
        <>
          <Button onClick={() => setOpen(true)}>Open details</Button>
          {open ? (
            <Modal title="Details" onClose={() => setOpen(false)}>
              <p>Record</p>
            </Modal>
          ) : null}
        </>
      );
    }
    render(<Example />);
    const opener = screen.getByRole("button", { name: "Open details" });
    opener.focus();
    fireEvent.click(opener);
    const close = screen.getByRole("button", { name: "Close" });
    close.focus();
    fireEvent.click(close);
    expect(opener).toHaveFocus();
  });
  it("secondary buttons do not accidentally submit their containing form", () => {
    const submit = vi.fn((e) => e.preventDefault());
    render(
      <form onSubmit={submit}>
        <Button onClick={() => {}}>Cancel</Button>
        <Button type="submit">Save</Button>
      </form>,
    );
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(submit).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(submit).toHaveBeenCalledTimes(1);
  });
  it("shows denial instead of false empty state or stale content", () => {
    render(
      <QueryState
        query={{
          isPending: false,
          error: Error("Not permitted"),
          items: [],
          refetch: vi.fn(),
        }}
      >
        <p>Private record</p>
      </QueryState>,
    );
    expect(screen.getByRole("alert")).toHaveTextContent("Not permitted");
    expect(screen.queryByText("Private record")).not.toBeInTheDocument();
    expect(screen.queryByText("No matching records")).not.toBeInTheDocument();
  });
});
