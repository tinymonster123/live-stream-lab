import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { MeetingPanel } from "./MeetingPanel";

describe("MeetingPanel", () => {
  it("renders the create and join controls", () => {
    const html = renderToString(<MeetingPanel />);

    expect(html).toContain("Meeting");
    expect(html).toContain("Host name");
    expect(html).toContain("Create Meeting");
    expect(html).toContain("Room code");
    expect(html).toContain("Join Room");
  });
});
