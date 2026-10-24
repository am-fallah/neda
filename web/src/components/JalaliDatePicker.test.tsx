import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { JalaliDatePicker } from "./JalaliDatePicker";

describe("JalaliDatePicker", () => {
  it("shows the Jalali and Gregorian forms of the selected date", () => {
    render(
      <JalaliDatePicker
        id="departureDate"
        value="2026-10-20"
        onChange={() => {}}
      />,
    );
    expect(screen.getByText("28 Mehr 1405")).toBeInTheDocument();
    expect(screen.getByText("Gregorian 2026-10-20")).toBeInTheDocument();
    expect(screen.getByRole("button")).toHaveAttribute("id", "departureDate");
  });

  it("selects a day and returns a Gregorian ISO value", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <JalaliDatePicker
        id="departureDate"
        value="2026-10-20"
        onChange={onChange}
        min="2026-10-01"
      />,
    );
    await user.click(screen.getByRole("button", { name: /28 Mehr 1405/ }));
    await user.click(
      screen.getByRole("button", {
        name: /29 Mehr 1405 \(2026-10-21\)/,
      }),
    );
    expect(onChange).toHaveBeenCalledWith("2026-10-21");
  });

  it("disables days before the minimum date", async () => {
    const user = userEvent.setup();
    render(
      <JalaliDatePicker
        id="departureDate"
        value="2026-10-20"
        onChange={() => {}}
        min="2026-10-22"
      />,
    );
    await user.click(screen.getByRole("button", { name: /28 Mehr 1405/ }));
    expect(
      screen.getByRole("button", { name: /28 Mehr 1405 \(2026-10-20\)/ }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: /30 Mehr 1405 \(2026-10-22\)/ }),
    ).not.toBeDisabled();
  });

  it("marks invalid state for assistive technology", () => {
    render(
      <JalaliDatePicker
        id="departureDate"
        value=""
        onChange={() => {}}
        invalid
        describedBy="departureDate-error"
      />,
    );
    const trigger = screen.getByRole("button");
    expect(trigger).toHaveAttribute("aria-invalid", "true");
    expect(trigger).toHaveAttribute("aria-describedby", "departureDate-error");
  });
});
