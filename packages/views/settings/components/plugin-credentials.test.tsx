import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";
import { PluginCredentials } from "./plugin-credentials";

vi.mock("@multica/core/api", () => ({ api: { rotatePluginToken: vi.fn() } }));
const TOKEN = "test-bearer-private";
const SECRET = "whsec_" + "a".repeat(64);
const response = { token: TOKEN, signing_secret: SECRET };
let client: QueryClient;
function Wrapper({ children }: { children: ReactNode }) {
  return <QueryClientProvider client={client}><I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings } }}>{children}</I18nProvider></QueryClientProvider>;
}
async function open(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: "Credentials" }));
}
async function issue(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: "Issue / rotate bearer token" }));
}
beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  vi.mocked(api.rotatePluginToken).mockReset().mockResolvedValue(response);
});
afterEach(() => { cleanup(); client.clear(); });

describe("PluginCredentials", () => {
  it("requires confirmation, describes token and signing lifecycles, and cancels without a request", async () => {
    const user = userEvent.setup();
    render(<PluginCredentials wsId="ws-1" installationId="install-1" />, { wrapper: Wrapper });
    expect(api.rotatePluginToken).not.toHaveBeenCalled();
    await open(user);
    expect(screen.getByText(/does not revoke that secret/)).toBeInTheDocument();
    expect(screen.getByText("install-1")).toBeInTheDocument();
    expect(api.rotatePluginToken).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(api.rotatePluginToken).not.toHaveBeenCalled();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("masks credentials, reveals explicitly, copies only on click, and retains no mutation data", async () => {
    const user = userEvent.setup();
    const copy = vi.spyOn(navigator.clipboard, "writeText");
    render(<PluginCredentials wsId="ws-1" installationId="install-1" />, { wrapper: Wrapper });
    await open(user); await issue(user);
    expect(api.rotatePluginToken).toHaveBeenCalledExactlyOnceWith("ws-1", "install-1");
    expect(screen.getByLabelText("Signing secret")).toHaveAttribute("type", "password");
    expect(screen.getByLabelText("Bearer token")).toHaveAttribute("type", "password");
    expect(copy).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "Show or hide Signing secret" }));
    expect(screen.getByLabelText("Signing secret")).toHaveAttribute("type", "text");
    await user.click(screen.getByRole("button", { name: "Copy Signing secret" }));
    expect(copy).toHaveBeenCalledExactlyOnceWith(SECRET);
    await user.click(screen.getByRole("button", { name: "Copy installation ID" }));
    expect(copy).toHaveBeenLastCalledWith("install-1");
    await waitFor(() => expect(client.getMutationCache().getAll()).toHaveLength(0));
    expect(JSON.stringify(client.getQueryCache().getAll())).not.toContain(SECRET);
    await user.click(screen.getByRole("button", { name: "Close and clear" }));
    await open(user);
    expect(screen.queryByLabelText("Signing secret")).not.toBeInTheDocument();
    expect(api.rotatePluginToken).toHaveBeenCalledTimes(1);
  });

  it("clears previous values before a repeat request and keeps raw API errors out of UI and cache", async () => {
    const user = userEvent.setup();
    render(<PluginCredentials wsId="ws-1" installationId="install-1" />, { wrapper: Wrapper });
    await open(user); await issue(user);
    let reject!: (error: Error) => void;
    vi.mocked(api.rotatePluginToken).mockImplementationOnce(() => new Promise((_, fail) => { reject = fail; }));
    await issue(user);
    expect(screen.queryByLabelText("Signing secret")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Issue / rotate bearer token" })).toBeDisabled();
    await act(async () => reject(new Error(SECRET)));
    expect(await screen.findByRole("alert")).toHaveTextContent("previous bearer token may already have been replaced");
    expect(document.body.innerHTML).not.toContain(SECRET);
    expect(screen.getByRole("button", { name: "Cancel" })).toBeEnabled();
    await user.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await waitFor(() => expect(client.getMutationCache().getAll()).toHaveLength(0));
  });

  it("explains an older backend response without substituting its bearer for the signing secret", async () => {
    vi.mocked(api.rotatePluginToken).mockResolvedValue({ token: TOKEN, signing_secret: "" });
    const user = userEvent.setup();
    render(<PluginCredentials wsId="ws-1" installationId="install-1" />, { wrapper: Wrapper });
    await open(user); await issue(user);
    expect(screen.getByText(/This backend did not return a signing secret/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Signing secret")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Bearer token")).toHaveValue(TOKEN);
  });

  it.each(["workspace", "installation"])("clears on %s change and discards a late response", async (change) => {
    const user = userEvent.setup();
    const { rerender } = render(<PluginCredentials wsId="ws-1" installationId="install-1" />, { wrapper: Wrapper });
    await open(user); await issue(user);
    let resolve!: (value: typeof response) => void;
    vi.mocked(api.rotatePluginToken).mockImplementationOnce(() => new Promise((done) => { resolve = done; }));
    await issue(user);
    rerender(<PluginCredentials wsId={change === "workspace" ? "ws-2" : "ws-1"} installationId={change === "installation" ? "install-2" : "install-1"} />);
    await act(async () => resolve(response));
    expect(screen.queryByLabelText("Signing secret")).not.toBeInTheDocument();
    expect(document.body.innerHTML).not.toContain(SECRET);
    await waitFor(() => expect(client.getMutationCache().getAll()).toHaveLength(0));
  });

  it("clears on Escape and discards requests completed after close or unmount", async () => {
    const user = userEvent.setup();
    const { unmount } = render(<PluginCredentials wsId="ws-1" installationId="install-1" />, { wrapper: Wrapper });
    await open(user); await issue(user); await user.keyboard("{Escape}");
    expect(screen.queryByLabelText("Signing secret")).not.toBeInTheDocument();
    await open(user);
    let resolve!: (value: typeof response) => void;
    vi.mocked(api.rotatePluginToken).mockImplementationOnce(() => new Promise((done) => { resolve = done; }));
    await issue(user); unmount();
    await act(async () => resolve(response));
    expect(document.body.innerHTML).not.toContain(SECRET);
    await waitFor(() => expect(client.getMutationCache().getAll()).toHaveLength(0));
  });

  it.each(["cancel", "escape", "outside"])("blocks %s dismissal and repeat rotation while pending", async (dismissal) => {
    let resolve!: (value: typeof response) => void;
    vi.mocked(api.rotatePluginToken).mockImplementationOnce(() => new Promise((done) => { resolve = done; }));
    const user = userEvent.setup();
    render(<PluginCredentials wsId="ws-1" installationId="install-1" />, { wrapper: Wrapper });
    await open(user); await issue(user);
    const confirm = screen.getByRole("button", { name: "Issue / rotate bearer token" });
    expect(confirm).toBeDisabled();
    expect(confirm).toHaveAttribute("aria-busy", "true");
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
    if (dismissal === "cancel") await user.click(screen.getByRole("button", { name: "Cancel" }));
    else if (dismissal === "escape") await user.keyboard("{Escape}");
    else await user.click(document.querySelector('[data-slot="dialog-overlay"]')!);
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    await user.click(confirm);
    expect(api.rotatePluginToken).toHaveBeenCalledTimes(1);
    await act(async () => resolve(response));
    expect(await screen.findByLabelText("Signing secret")).toHaveAttribute("type", "password");
    await user.click(screen.getByRole("button", { name: "Close and clear" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await open(user);
    expect(screen.queryByLabelText("Signing secret")).not.toBeInTheDocument();
    expect(api.rotatePluginToken).toHaveBeenCalledTimes(1);
  });

  it("reports clipboard failure without exposing the value", async () => {
    const user = userEvent.setup();
    vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(new Error(SECRET));
    render(<PluginCredentials wsId="ws-1" installationId="install-1" />, { wrapper: Wrapper });
    await open(user); await issue(user);
    await user.click(screen.getByRole("button", { name: "Copy Signing secret" }));
    expect(await screen.findByRole("status")).toHaveTextContent("Copy failed");
    expect(screen.getByLabelText("Signing secret")).toHaveAttribute("type", "password");
  });
});
