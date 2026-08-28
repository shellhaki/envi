import { describe, expect, test } from "bun:test";
import { filterKeys, initials } from "./utils";

describe("dashboard helpers", () => {
  test("filters and sorts secret keys", () => {
    expect(filterKeys({ Z_KEY: "", API_URL: "", API_TOKEN: "" }, "api")).toEqual(["API_TOKEN", "API_URL"]);
  });

  test("builds stable account initials", () => {
    expect(initials("dev@example.com")).toBe("DE");
    expect(initials()).toBe("EN");
  });
});
