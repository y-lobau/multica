// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";
import { setSchemaLogger } from "./schema";
import { noopLogger } from "../logger";
const token = "test-private-token";
const signing_secret = "whsec_" + "a".repeat(64);
function respond(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => { vi.unstubAllGlobals(); setSchemaLogger(noopLogger); });
describe("plugin credential response boundary", () => {
  it("parses both credentials and strips extra fields", async () => {
    respond({ token, signing_secret, extra: "private-extra" });
    expect(await new ApiClient("https://example.test").rotatePluginToken("ws", "install")).toEqual({ token, signing_secret });
    expect(fetch).toHaveBeenCalledWith("https://example.test/api/workspaces/ws/plugins/install/token", expect.objectContaining({ method: "POST" }));
  });
  it("defaults an omitted signing secret for older servers", async () => {
    respond({ token });
    expect(await new ApiClient("https://example.test").rotatePluginToken("ws", "install")).toEqual({ token, signing_secret: "" });
  });
  it.each([null, {}, { token: 123, signing_secret }, { token, signing_secret: { secret: signing_secret } }, { token: "", signing_secret }])("rejects malformed credentials without logging values: %j", async (body) => {
    const warn = vi.fn(); setSchemaLogger({ ...noopLogger, warn });
    respond(body);
    expect(await new ApiClient("https://example.test").rotatePluginToken("ws", "install")).toEqual({ token: "", signing_secret: "" });
    expect(warn).toHaveBeenCalledExactlyOnceWith("API response failed schema validation: POST /api/workspaces/{id}/plugins/{installationId}/token");
    expect(JSON.stringify(warn.mock.calls)).not.toContain(signing_secret);
  });
});
