import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { Button, QueryState } from "../src/ui";
describe("shared primitives", () => {
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
