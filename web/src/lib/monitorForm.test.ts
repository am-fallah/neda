import { describe, expect, it } from "vitest";
import {
  buildMonitorPayload,
  emptyMonitorForm,
  mapServerFieldErrors,
  validateMonitorForm,
  type MonitorFormValues,
} from "./monitorForm";
import { makeProvider } from "@/test/fixtures";

const provider = makeProvider();

function validValues(): MonitorFormValues {
  return {
   ...emptyMonitorForm(),
    label: "  Tehran to Mashhad  ",
    originName: " Tehran ",
    destinationName: " Mashhad ",
    departureDate: "2026-10-28",
    adults: 2,
    children: 1,
    infants: 0,
    selectedProviderIds: ["fake"],
    providerConfigs: {
      fake: {
        originId: " test-origin-available ",
        destinationId: "test-destination-default",
        bookingSlug: "",
        genderCode: "",
      },
    },
  };
}

describe("monitor form validation", () => {
  it("requires the core fields", () => {
    const errors = validateMonitorForm(
      emptyMonitorForm(),
      [provider],
      new Date("2026-10-24T10:00:00Z"),
    );
    expect(errors.label).toBeDefined();
    expect(errors.originName).toBeDefined();
    expect(errors.destinationName).toBeDefined();
    expect(errors.departureDate).toBeDefined();
    expect(errors.providers).toBeDefined();
  });

  it("accepts a valid form", () => {
    const errors = validateMonitorForm(
      validValues(),
      [provider],
      new Date("2026-10-24T10:00:00Z"),
    );
    expect(errors).toEqual({});
  });

  it("rejects identical display names and provider route IDs", () => {
    const values = validValues();
    values.destinationName = " tehran ";
    values.providerConfigs.fake.destinationId = "test-origin-available";
    const errors = validateMonitorForm(
      values,
      [provider],
      new Date("2026-10-24T10:00:00Z"),
    );
    expect(errors.destinationName).toContain("differ");
    expect(errors["providerConfigs.fake.destinationId"]).toContain("differ");
  });

  it("rejects a departure date before today in Tehran", () => {
    const values = validValues();
    values.departureDate = "2026-10-20";
    const errors = validateMonitorForm(
      values,
      [provider],
      new Date("2026-10-20T21:30:00Z"),
    );
    expect(errors.departureDate).toContain("before today");
  });

  it("enforces passenger bounds and total", () => {
    const values = validValues();
    values.adults = 9;
    values.children = 1;
    const errors = validateMonitorForm(
      values,
      [provider],
      new Date("2026-10-24T10:00:00Z"),
    );
    expect(errors.passengers).toContain("at most 9");
  });

  it("rejects unsupported booking slugs and gender codes", () => {
    const values = validValues();
    values.providerConfigs.fake.bookingSlug = "some-slug";
    values.providerConfigs.fake.genderCode = "M";
    const errors = validateMonitorForm(
      values,
      [provider],
      new Date("2026-10-24T10:00:00Z"),
    );
    expect(errors["providerConfigs.fake.bookingSlug"]).toContain(
      "does not support",
    );
    expect(errors["providerConfigs.fake.genderCode"]).toContain(
      "does not support",
    );
  });

  it("rejects control characters in optional provider fields", () => {
    const supporting = makeProvider({
      id: "real",
      testOnly: false,
      capabilities: {
        ...provider.capabilities,
        supportsBookingUrl: true,
        supportsGender: true,
      },
    });
    const values = validValues();
    values.selectedProviderIds = ["real"];
    values.providerConfigs = {
      real: {
        originId: "1",
        destinationId: "2",
        bookingSlug: "bad\u0000slug",
        genderCode: "ok",
      },
    };
    const errors = validateMonitorForm(
      values,
      [supporting],
      new Date("2026-10-24T10:00:00Z"),
    );
    expect(errors["providerConfigs.real.bookingSlug"]).toContain(
      "Control characters",
    );
  });
});

describe("monitor payload", () => {
  it("trims fields and sends canonical Gregorian dates and provider IDs", () => {
    const payload = buildMonitorPayload(validValues());
    expect(payload).toEqual({
      label: "Tehran to Mashhad",
      originName: "Tehran",
      destinationName: "Mashhad",
      departureDate: "2026-10-28",
      adults: 2,
      children: 1,
      infants: 0,
      providers: [
        {
          providerId: "fake",
          originId: "test-origin-available",
          destinationId: "test-destination-default",
        },
      ],
    });
  });

  it("omits empty optional provider fields", () => {
    const payload = buildMonitorPayload(validValues());
    expect(payload.providers[0]).not.toHaveProperty("bookingSlug");
    expect(payload.providers[0]).not.toHaveProperty("genderCode");
  });
});

describe("server field errors", () => {
  it("maps indexed provider errors to form keys in payload order", () => {
    const second = makeProvider({ id: "fake-2", displayName: "Second" });
    const result = mapServerFieldErrors(
      {
        "providers[1].originId": "provider is not registered",
        "providers[0]": "provider configuration rejected",
        passengers: "total must be at most 9",
        departureDate: "before today",
      },
      [provider, second],
    );
    expect(result.errors["providerConfigs.fake.originId"]).toBeUndefined();
    expect(result.errors["providerConfigs.fake-2.originId"]).toBe(
      "provider is not registered",
    );
    expect(result.errors["provider.fake"]).toContain("rejected");
    expect(result.errors.passengers).toContain("at most 9");
    expect(result.errors.departureDate).toContain("before today");
    expect(result.general).toHaveLength(1);
  });
});
