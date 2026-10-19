import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { App } from "./App";

describe("App", () => {
  it("renders the project name", () => {
    expect(renderToString(<App />)).toContain("Live Stream Lab");
  });

  it("renders a placeholder for infrastructure status", () => {
    const html = renderToString(<App />);

    expect(html).toContain("Infrastructure status");
    expect(html).toContain("unknown");
  });
});
