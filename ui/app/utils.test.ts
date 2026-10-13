import { describe, expect, test } from "bun:test";
import { filterKeys, formatDotenv, initials, parseDotenv, relativeTime } from "./utils";

describe("dashboard helpers", () => {
  test("filters and sorts secret keys", () => {
    expect(filterKeys({ Z_KEY: "", API_URL: "", API_TOKEN: "" }, "api")).toEqual(["API_TOKEN", "API_URL"]);
  });

  test("builds stable account initials", () => {
    expect(initials("dev@example.com")).toBe("DE");
    expect(initials()).toBe("EN");
  });
});

describe("parseDotenv", () => {
  test("parses plain KEY=VALUE lines", () => {
    expect(parseDotenv("API_KEY=abc123\nPORT=8080")).toEqual({ API_KEY: "abc123", PORT: "8080" });
  });

  test("ignores blanks and comments", () => {
    expect(parseDotenv("# heading\n\nA=1\n  # indented comment\nB=2")).toEqual({ A: "1", B: "2" });
  });

  test("strips an optional export prefix", () => {
    expect(parseDotenv("export TOKEN=xyz")).toEqual({ TOKEN: "xyz" });
  });

  test("keeps = and : inside values", () => {
    expect(parseDotenv("DATABASE_URL=postgres://u:p@host:5432/db?x=1")).toEqual({
      DATABASE_URL: "postgres://u:p@host:5432/db?x=1",
    });
  });

  test("unwraps quotes and honors escapes in double quotes", () => {
    expect(parseDotenv(`A="hello world"\nB='raw #notacomment'\nC="line\\nbreak"`)).toEqual({
      A: "hello world",
      B: "raw #notacomment",
      C: "line\nbreak",
    });
  });

  test("drops inline comments from unquoted values only", () => {
    expect(parseDotenv("A=value # trailing\nB=has#hash")).toEqual({ A: "value", B: "has#hash" });
  });

  test("skips invalid identifiers and keyless lines", () => {
    expect(parseDotenv("123BAD=x\nnokeyhere\nGOOD=1")).toEqual({ GOOD: "1" });
  });

  test("allows empty values", () => {
    expect(parseDotenv("EMPTY=")).toEqual({ EMPTY: "" });
  });
});

describe("formatDotenv", () => {
  test("sorts keys and writes plain values bare", () => {
    expect(formatDotenv({ PORT: "8080", API_KEY: "abc123" })).toBe("API_KEY=abc123\nPORT=8080");
  });

  test("leaves an empty value unquoted", () => {
    expect(formatDotenv({ EMPTY: "" })).toBe("EMPTY=");
  });

  test("quotes values that would not survive bare", () => {
    expect(formatDotenv({ KEY: "line1\nline2" })).toBe('KEY="line1\\nline2"');
    expect(formatDotenv({ PAD: " spaced " })).toBe('PAD=" spaced "');
    expect(formatDotenv({ Q: 'has "quotes"' })).toBe('Q="has \\"quotes\\""');
  });

  test("does not quote values that merely contain = # or :", () => {
    const url = "postgres://u:p@host:5432/db?x=1#frag";
    expect(formatDotenv({ DATABASE_URL: url })).toBe(`DATABASE_URL=${url}`);
  });

  test("round-trips through parseDotenv", () => {
    const values = { A: "plain", B: "line1\nline2", C: 'with "quotes"', D: "", E: "a\\b" };
    expect(parseDotenv(formatDotenv(values))).toEqual(values);
  });

  test("renders nothing for an empty set", () => {
    expect(formatDotenv({})).toBe("");
  });
});

describe("relativeTime", () => {
  test("renders recent timestamps", () => {
    expect(relativeTime(new Date().toISOString())).toBe("just now");
    expect(relativeTime(new Date(Date.now() - 5 * 60_000).toISOString())).toBe("5m ago");
    expect(relativeTime(new Date(Date.now() - 3 * 3600_000).toISOString())).toBe("3h ago");
  });

  test("returns empty for an unparseable value", () => {
    expect(relativeTime("not-a-date")).toBe("");
  });
});
