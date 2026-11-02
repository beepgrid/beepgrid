import { useState } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import {
  Button,
  ConversationAvatar,
  conversationPeers,
  conversationLabel,
  IdentityIcon,
  Modal,
  QueryState,
  RequestKind,
  RequestStatus,
  ConversationPreview,
} from "../src/ui";
describe("shared primitives", () => {
  it("labels recorded outcomes while keeping pending requests and ordinary previews distinct", () => {
    const { rerender } = render(
      <RequestStatus
        request={{ state: "resolved", response: { outcome: "answered" } }}
      />,
    );
    expect(screen.getByText("Replied")).toHaveClass("good");
    rerender(
      <RequestStatus
        request={{ state: "expired", response: { outcome: "approved" } }}
      />,
    );
    expect(screen.getByText("Expired")).not.toHaveClass("good");
    rerender(
      <ConversationPreview body="The request was answered yesterday." />,
    );
    expect(
      screen.getByText("The request was answered yesterday."),
    ).not.toHaveClass("badge");
    rerender(<ConversationPreview body="open" />);
    expect(screen.getByText("open")).not.toHaveClass("badge");
  });
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

const self = { id: "person_self", kind: "person" as const, name: "Alex Gray" };
const scout = { id: "agent_scout", kind: "agent" as const, name: "Scout" };
const pat = { id: "person_pat", kind: "person" as const, name: "Pat Chen" };
it("uses only actual distinct conversation peers, with a safe self-only fallback", () => {
  expect(conversationPeers([self, scout, scout], self.id)).toEqual([scout]);
  expect(conversationPeers([self, scout, pat], self.id)).toEqual([scout, pat]);
  expect(conversationPeers([self], self.id)).toEqual([self]);
  expect(conversationPeers([], self.id)).toEqual([]);
});
it("represents a private direct conversation as one identity and a real multi-peer conversation as a collage", () => {
  const { container, rerender } = render(
    <ConversationAvatar participants={[self, scout]} currentId={self.id} />,
  );
  expect(container.querySelector(".conversation-avatar")).toBeNull();
  expect(container.querySelectorAll(".identity-icon")).toHaveLength(1);
  rerender(
    <ConversationAvatar
      participants={[self, scout, pat]}
      currentId={self.id}
    />,
  );
  expect(
    container.querySelectorAll(".conversation-avatar .identity-icon"),
  ).toHaveLength(2);
  expect(container).toHaveTextContent("PC");
  expect(container).not.toHaveTextContent("AG");
});
it("keeps identity color stable when a known identity is renamed and labels request kinds without relying on color", () => {
  const { container, rerender } = render(
    <IdentityIcon id={scout.id} label="Scout" />,
  );
  const tone = container.firstElementChild?.getAttribute("data-tone");
  rerender(<IdentityIcon id={scout.id} label="Research assistant" />);
  expect(container.firstElementChild?.getAttribute("data-tone")).toBe(tone);
  rerender(
    <>
      <RequestKind kind="question" />
      <RequestKind kind="approval" />
      <RequestKind kind="task" />
    </>,
  );
  expect(screen.getByText("question")).toBeVisible();
  expect(screen.getByText("approval")).toBeVisible();
  expect(screen.getByText("task")).toBeVisible();
});

it("labels participant conversations truthfully for participants and administrative observers", () => {
  expect(conversationLabel([self, scout], self.id)).toBe("agent");
  expect(conversationLabel([self, scout], "person_observer")).toBe(
    "2 participants",
  );
  expect(conversationLabel([self, scout, pat], self.id)).toBe("3 participants");
});

it("gives an agent a stable visual mark across renames and keeps people distinct", () => {
  const { container, rerender } = render(
    <IdentityIcon id="agent_42" label="Scout" />,
  );
  const before = container.innerHTML;
  expect(container.querySelector(".agent-mark")).not.toBeNull();
  rerender(<IdentityIcon id="agent_42" label="A completely different name" />);
  expect(container.innerHTML).toBe(before);
  rerender(<IdentityIcon id="agent_42" kind="person" label="Pat Chen" />);
  expect(container.querySelector(".agent-mark")).toBeNull();
  expect(container.querySelector(".identity-person")).toHaveTextContent("PC");
  rerender(<IdentityIcon id="agent_42" kind="service" label="Scout" />);
  expect(container.querySelector(".agent-mark")).toBeNull();
  expect(container.querySelector(".identity-service")).not.toBeNull();
});

it("keeps participant collages stable when API ordering changes and never mutates the roster", () => {
  const roster = Object.freeze([self, scout, pat]);
  const { container, rerender } = render(
    <ConversationAvatar participants={roster} currentId={self.id} />,
  );
  const before = container.innerHTML;
  rerender(
    <ConversationAvatar
      participants={[pat, scout, self, scout]}
      currentId={self.id}
    />,
  );
  expect(container.innerHTML).toBe(before);
  expect(roster).toEqual([self, scout, pat]);
});

it("bounds larger collages with an accurate remainder and uses a neutral empty fallback", () => {
  const peers = Array.from({ length: 7 }, (_, i) => ({
    id: `agent_${i}`,
    kind: "agent" as const,
    name: `Worker ${i}`,
  }));
  const { container, rerender } = render(
    <ConversationAvatar participants={[self, ...peers]} currentId={self.id} />,
  );
  expect(container.querySelectorAll(".agent-mark")).toHaveLength(3);
  expect(container.querySelector(".avatar-more")).toHaveTextContent("+4");
  expect(container.querySelectorAll(".conversation-avatar > *")).toHaveLength(
    4,
  );
  rerender(<ConversationAvatar participants={[]} currentId={self.id} />);
  expect(container.querySelector(".identity-conversation")).not.toBeNull();
  expect(container.querySelector(".agent-mark")).toBeNull();
});
