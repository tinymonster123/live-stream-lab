import { describe, expect, it, vi } from "vitest";
import { absoluteMediaUrl, postSdp } from "./media";

describe("postSdp", () => {
  it("returns the answer and the session resource from Location", async () => {
    const fetchImpl = vi.fn(
      async () =>
        new Response("v=0\r\n", {
          status: 201,
          headers: { Location: "/rtc/v1/whip/?action=delete&session=1" },
        }),
    );

    const exchange = await postSdp(
      "http://127.0.0.1:1985/rtc/v1/whip/?app=live&stream=p1",
      "v=0\r\n",
      fetchImpl as unknown as typeof fetch,
    );

    expect(exchange.answer).toBe("v=0\r\n");
    expect(exchange.resourceUrl).toBe("http://127.0.0.1:1985/rtc/v1/whip/?action=delete&session=1");
    expect(fetchImpl).toHaveBeenCalledWith(
      "http://127.0.0.1:1985/rtc/v1/whip/?app=live&stream=p1",
      expect.objectContaining({
        method: "POST",
        headers: { "Content-Type": "application/sdp" },
        body: "v=0\r\n",
      }),
    );
  });

  it("reports the media server's rejection", async () => {
    const fetchImpl = vi.fn(async () => new Response("stream not found", { status: 404 }));

    await expect(
      postSdp(
        "http://127.0.0.1:1985/rtc/v1/whep/?app=live&stream=p1",
        "v=0",
        fetchImpl as unknown as typeof fetch,
      ),
    ).rejects.toThrow("Media server rejected the SDP (HTTP 404): stream not found");
  });

  it("returns no resource when the server omits Location", async () => {
    const fetchImpl = vi.fn(async () => new Response("v=0", { status: 200 }));

    const exchange = await postSdp(
      "http://127.0.0.1:1985/rtc/v1/whep/?app=live&stream=p1",
      "v=0",
      fetchImpl as unknown as typeof fetch,
    );

    expect(exchange.resourceUrl).toBeNull();
  });
});

describe("absoluteMediaUrl", () => {
  it("resolves the control plane's relative path against the page origin", () => {
    expect(absoluteMediaUrl("/rtc/v1/whep/?app=live&stream=p1", "https://192.168.1.5:8443/")).toBe(
      "https://192.168.1.5:8443/rtc/v1/whep/?app=live&stream=p1",
    );
  });

  it("keeps an absolute media URL unchanged", () => {
    expect(absoluteMediaUrl("http://127.0.0.1:1985/rtc/v1/whip/", "https://example.test/")).toBe(
      "http://127.0.0.1:1985/rtc/v1/whip/",
    );
  });
});
