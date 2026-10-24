import { describe, expect, it } from "vitest";
import {
  isPushSupported,
  pushSupportMessage,
  urlBase64ToUint8Array,
} from "./push";

describe("push helpers", () => {
  it("converts a base64url VAPID key to bytes", () => {
    const bytes = urlBase64ToUint8Array("AQIDBA");
    expect(Array.from(bytes)).toEqual([1, 2, 3, 4]);
  });

  it("accepts standard base64 alphabet and padding", () => {
    const bytes = urlBase64ToUint8Array("-_8=");
    expect(Array.from(bytes)).toEqual([0xfb, 0xff]);
  });

  it("reports unsupported browsers without throwing", () => {
    expect(isPushSupported()).toBe(false);
    expect(pushSupportMessage()).not.toBe("");
  });
});
