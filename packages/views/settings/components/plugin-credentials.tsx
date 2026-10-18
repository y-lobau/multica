"use client";

import { useEffect, useId, useRef, useState } from "react";
import { useRotatePluginToken } from "@multica/core/plugins";
import type { PluginTokenIssue } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter,
  DialogHeader, DialogTitle, DialogTrigger,
} from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";

export function PluginCredentials({ wsId, installationId }: { wsId: string; installationId: string }) {
  const { t } = useT("settings");
  const [open, setOpen] = useState(false);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger render={<Button variant="outline" size="sm" />}>
        {t(($) => $.plugins.credentials.open)}
      </DialogTrigger>
      {open ? (
        <CredentialSession key={`${wsId}:${installationId}`} wsId={wsId} installationId={installationId} onClose={() => setOpen(false)} />
      ) : null}
    </Dialog>
  );
}

function CredentialSession({ wsId, installationId, onClose }: {
  wsId: string; installationId: string; onClose: () => void;
}) {
  const { t } = useT("settings");
  const [credentials, setCredentials] = useState<PluginTokenIssue | null>(null);
  const [error, setError] = useState(false);
  const [copyState, setCopyState] = useState<"copied" | "failed" | null>(null);
  const active = useRef(false);
  const mutation = useRotatePluginToken(wsId, (value) => {
    if (active.current) setCredentials(value);
  });
  const { reset } = mutation;
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
      reset();
    };
  }, [reset]);

  const issue = async () => {
    if (mutation.isPending) return;
    setCredentials(null);
    setError(false);
    setCopyState(null);
    try {
      await mutation.mutateAsync(installationId);
    } catch {
      if (active.current) setError(true);
    } finally {
      reset();
    }
  };

  const copy = async (value: string) => {
    setCopyState(null);
    try {
      await navigator.clipboard.writeText(value);
      if (active.current) setCopyState("copied");
    } catch {
      if (active.current) setCopyState("failed");
    }
  };

  return (
    <DialogContent showCloseButton={false} className="max-h-[calc(100dvh_-_2rem)] w-[calc(100vw_-_2rem)]">
      <DialogHeader>
        <DialogTitle>{t(($) => $.plugins.credentials.title)}</DialogTitle>
        <DialogDescription>{t(($) => $.plugins.credentials.consequence)}</DialogDescription>
      </DialogHeader>
      <div className="min-w-0 space-y-3">
        <div className="text-caption font-medium">{t(($) => $.plugins.credentials.installation_id)}</div>
        <code className="block break-all text-caption">{installationId}</code>
        <Button variant="outline" size="sm" onClick={() => void copy(installationId)}>
          {t(($) => $.plugins.credentials.copy_id)}
        </Button>
        {credentials ? (
          <div className="space-y-3">
            <p className="text-caption text-muted-foreground">{t(($) => $.plugins.credentials.private_hint)}</p>
            <CredentialValue label={t(($) => $.plugins.credentials.bearer)} value={credentials.token} onCopy={copy} />
            {credentials.signing_secret ? (
              <CredentialValue label={t(($) => $.plugins.credentials.signing_secret)} value={credentials.signing_secret} onCopy={copy} />
            ) : <p role="status" className="text-caption">{t(($) => $.plugins.credentials.no_signing_secret)}</p>}
          </div>
        ) : null}
        {error ? <p role="alert" className="text-caption text-destructive">{t(($) => $.plugins.credentials.failed)}</p> : null}
        {copyState ? <p role="status" className="text-caption">{copyState === "copied"
          ? t(($) => $.plugins.credentials.copied) : t(($) => $.plugins.credentials.copy_failed)}</p> : null}
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          {credentials ? t(($) => $.plugins.credentials.close) : t(($) => $.plugins.consent.cancel)}
        </Button>
        <Button variant="destructive" disabled={mutation.isPending} aria-busy={mutation.isPending} onClick={() => void issue()}>
          {t(($) => $.plugins.credentials.confirm)}
        </Button>
      </DialogFooter>
    </DialogContent>
  );
}

function CredentialValue({ label, value, onCopy }: { label: string; value: string; onCopy: (value: string) => Promise<void> }) {
  const { t } = useT("settings");
  const [revealed, setRevealed] = useState(false);
  const id = useId();
  return (
    <div className="min-w-0 space-y-2">
      <label htmlFor={id} className="text-caption font-medium">{label}</label>
      <Input id={id} type={revealed ? "text" : "password"} readOnly value={value} autoComplete="off" spellCheck={false} />
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" size="sm" aria-label={t(($) => $.plugins.credentials.toggle_label, { label })} aria-pressed={revealed} onClick={() => setRevealed(!revealed)}>
          {revealed ? t(($) => $.plugins.credentials.hide) : t(($) => $.plugins.credentials.reveal)}
        </Button>
        <Button variant="outline" size="sm" aria-label={t(($) => $.plugins.credentials.copy_label, { label })} onClick={() => void onCopy(value)}>
          {t(($) => $.plugins.credentials.copy)}
        </Button>
      </div>
    </div>
  );
}
