import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import {
  DeliveryStatusBadge,
  ObservationStateBadge,
  ProviderHealthBadge,
} from "./status";
import { makeProvider } from "@/test/fixtures";

describe("delivery status language", () => {
  it.each([
    ["pending", "Queued"],
    ["sending", "Sending"],
    ["sent", "Sent"],
    ["retry_scheduled", "Retry scheduled"],
    ["failed", "Failed"],
    ["skipped", "Skipped"],
  ] as const)("renders %s as %s", (status, label) => {
    render(<DeliveryStatusBadge status={status} />);
    expect(screen.getByText(label)).toBeInTheDocument();
    if (status !== "sent") {
      expect(screen.queryByText("Sent")).not.toBeInTheDocument();
    }
  });
});

describe("availability status language", () => {
  it("distinguishes reservable, not reservable, and unknown", () => {
    const { rerender } = render(<ObservationStateBadge state="reservable" />);
    expect(screen.getByText("Reservable")).toBeInTheDocument();

    rerender(<ObservationStateBadge state="not_reservable" />);
    expect(screen.getByText("Not reservable")).toBeInTheDocument();
    expect(screen.queryByText(/sold out/i)).not.toBeInTheDocument();

    rerender(<ObservationStateBadge state="unknown" />);
    expect(screen.getByText("Unknown")).toBeInTheDocument();
  });

  it("marks missing observations as unknown", () => {
    render(<ObservationStateBadge state="not_reservable" missing />);
    expect(screen.getByText(/Unknown · not returned/)).toBeInTheDocument();
    expect(screen.queryByText("Not reservable")).not.toBeInTheDocument();
  });
});

describe("provider health badges", () => {
  it("separates degraded, stale, healthy, and unknown health", () => {
    const { rerender } = render(
      <ProviderHealthBadge
        provider={makeProvider({
          health: {
            ...makeProvider().health!,
            openEpisode: {
              id: 1,
              openedAt: "2026-10-24T10:00:00Z",
              failureCountAtOpen: 3,
              lastErrorKind: "timeout",
            },
          },
        })}
      />,
    );
    expect(screen.getByText("Degraded")).toBeInTheDocument();

    rerender(
      <ProviderHealthBadge
        provider={makeProvider({
          health: { ...makeProvider().health!, stale: true },
        })}
      />,
    );
    expect(screen.getByText("Stale")).toBeInTheDocument();

    rerender(
      <ProviderHealthBadge
        provider={makeProvider({
          health: { ...makeProvider().health!, lastSuccessAt: null },
        })}
      />,
    );
    expect(screen.getByText("No success yet")).toBeInTheDocument();

    rerender(<ProviderHealthBadge provider={makeProvider({ health: null })} />);
    expect(screen.getByText("No checks yet")).toBeInTheDocument();
  });
});
