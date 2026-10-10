import { ConfirmActionDialog } from "@/components/confirm-action-dialog";
import { useEffect, useId, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import QRCode from "qrcode";
import { CircleHelp, X } from "lucide-react";
import { Badge } from "./ui/badge";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "./ui/tooltip";
import { api } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { DeliverySettings } from "@/lib/types";
import { Button } from "./ui/button";
import { Checkbox } from "./ui/checkbox";
import { Input } from "./ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "./ui/table";
import {
  Field,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "./ui/field";
import { Alert, AlertDescription } from "./ui/alert";
import { BusyButton, ErrorNotice, Panel, Time } from "./common";
export type NativePairing = {
  id: string;
  status: "waiting" | "active" | "expired" | "failed" | "revoked";
  device_name: string;
  device_id?: string;
  expires_at: string;
  revoked_at?: string;
  error_code?: string;
  uri?: string;
  fingerprint?: string;
  /** Set for a management-only invitation, whose acceptance is tracked by its invitation ID. */
  managementOnly?: boolean;
};

export function PairingDialog({
  created,
  onClose,
}: {
  created: NativePairing;
  onClose: () => void;
}) {
  const { t } = useI18n();
  const client = useQueryClient();
  const dialog = useRef<HTMLDialogElement>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  const title = useId();
  const description = useId();
  const [now, setNow] = useState(() => Date.now());
  const [image, setImage] = useState("");
  const [qrError, setQRError] = useState<unknown>();
  const [copied, setCopied] = useState(false);
  const [manualCopy, setManualCopy] = useState(false);
  const query = useQuery({
    queryKey: [
      created.managementOnly ? "management-invitation" : "native-pairing",
      created.id,
    ],
    queryFn: ({ signal }) =>
      api<NativePairing>(
        `${created.managementOnly ? "/device-invitations" : "/native/pairings"}/${encodeURIComponent(created.id)}`,
        {
          signal,
        },
      ),
    refetchInterval: (query) =>
      !query.state.data || query.state.data.status === "waiting" ? 3000 : false,
    refetchIntervalInBackground: false,
  });
  const current = query.data ?? created;
  const remaining = Math.max(
    0,
    Math.ceil((Date.parse(created.expires_at) - now) / 1000),
  );
  const status =
    current.status === "waiting" && remaining === 0
      ? "expired"
      : current.status;
  useEffect(() => {
    dialog.current?.showModal();
    heading.current?.focus();
  }, []);
  useEffect(() => {
    if (status !== "waiting") return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [status]);
  useEffect(() => {
    let active = true;
    if (created.uri)
      void QRCode.toString(created.uri, {
        type: "svg",
        errorCorrectionLevel: "M",
        margin: 4,
      })
        .then((svg) => {
          if (active)
            setImage(
              `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`,
            );
        })
        .catch((error) => {
          if (active) setQRError(error);
        });
    return () => {
      active = false;
    };
  }, [created.uri]);
  useEffect(() => {
    if (current.status === "active") {
      void client.invalidateQueries({ queryKey: ["native-devices"] });
      void client.invalidateQueries({ queryKey: ["management-devices"] });
    }
  }, [current.status, client]);
  async function copyLink() {
    try {
      await navigator.clipboard.writeText(created.uri!);
      setCopied(true);
      setManualCopy(false);
    } catch {
      setCopied(false);
      setManualCopy(true);
    }
  }
  return (
    <dialog
      ref={dialog}
      className="relative m-auto max-h-[calc(100dvh-2rem)] w-[min(25rem,calc(100vw-2rem))] overflow-hidden rounded-lg border bg-card p-0 text-card-foreground backdrop:bg-[var(--dialog-backdrop)]"
      aria-labelledby={title}
      aria-describedby={description}
      onClose={onClose}
    >
      <div className="flex max-h-[calc(100dvh-2rem)] flex-col">
        <header className="flex shrink-0 flex-col items-center gap-2 px-6 pt-6 text-center">
          <h2
            ref={heading}
            tabIndex={-1}
            id={title}
            className="text-lg font-semibold"
          >
            {t("ui.pair_phone")}
          </h2>
          <div className="flex flex-wrap items-center justify-center gap-x-2 gap-y-1 text-sm text-muted-foreground">
            <p role="status">
              {t(`ui.pairing_${status}`)}
              {status === "active" && current.device_name
                ? ` · ${current.device_name}`
                : ""}
            </p>
            {status === "waiting" && (
              <>
                <span aria-hidden="true">·</span>
                <span className="tabular-nums">
                  {t("ui.pairing_seconds", { seconds: String(remaining) })}
                </span>
              </>
            )}
          </div>
          <button
            type="button"
            aria-label={t("ui.close")}
            className="absolute right-3 top-3 flex size-8 items-center justify-center rounded-sm text-muted-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring"
            onClick={() => dialog.current?.close()}
          >
            <X className="size-4" aria-hidden="true" />
          </button>
        </header>
        <div className="flex min-h-0 flex-1 flex-col items-center gap-5 overflow-y-auto overscroll-contain px-6 pb-7 pt-0 text-center">
          <ErrorNotice error={query.error ?? qrError} />
          {status === "waiting" && image && (
            <div className="flex w-full flex-col items-center gap-0">
              <button
                type="button"
                className="block w-full max-w-[280px] cursor-copy rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
                aria-label={t(copied ? "ui.copied" : "ui.copy_connection_link")}
                title={t("ui.copy_connection_link")}
                onClick={() => void copyLink()}
              >
                <img
                  className="block h-auto w-full"
                  src={image}
                  alt={t("ui.pairing_qr")}
                  width={280}
                  height={280}
                />
              </button>
              <p aria-live="polite" className="text-xs text-muted-foreground">
                {t(copied ? "ui.copied" : "ui.copy_connection_link_hint")}
              </p>
              {manualCopy && (
                <>
                  <Input
                    aria-label={t("ui.copy_connection_link")}
                    readOnly
                    value={created.uri}
                    onFocus={(event) => event.currentTarget.select()}
                  />
                  <p
                    role="status"
                    className="mt-2 text-sm text-muted-foreground"
                  >
                    {t("ui.copy_connection_link_manually")}
                  </p>
                </>
              )}
            </div>
          )}
          <div className="flex flex-col items-center gap-3 text-xs leading-relaxed text-muted-foreground">
            <p id={description}>{t("ui.pairing_warning")}</p>
            <p className="flex flex-wrap justify-center gap-x-2">
              <span>{t("ui.core_fingerprint")}</span>
              <span className="font-mono tabular-nums">
                {created.fingerprint}
              </span>
            </p>
          </div>
          {current.error_code && (
            <Alert variant="destructive">
              <AlertDescription>{t(current.error_code)}</AlertDescription>
            </Alert>
          )}
        </div>
      </div>
    </dialog>
  );
}

type Invitation = {
  invitation_id: string;
  uri: string;
  fingerprint: string;
  pairing_id?: string;
  expires_at: string;
};
type ManagementDevice = {
  controller_id: string;
  device_id: string;
  device_name: string;
  scopes: string[];
  created_at: string;
  last_used_at?: string;
};
export function ManagementDevices() {
  const { t } = useI18n();
  const devices = useQuery({
    queryKey: ["management-devices"],
    queryFn: ({ signal }) =>
      api<{ devices: ManagementDevice[] }>("/management/devices", { signal }),
  });
  const settings = useQuery({
    queryKey: ["delivery-settings"],
    queryFn: () => api<DeliverySettings>("/settings/delivery"),
  });
  const [management, setManagement] = useState(true);
  const [nativeSelected, setNative] = useState(true);
  const native = nativeSelected && !!settings.data?.native_available;
  const [origin, setOrigin] = useState(() =>
    window.location.protocol === "https:" ? window.location.origin : "",
  );
  const [scopes, setScopes] = useState([
    "channels",
    "content",
    "diagnostics",
    "folders",
    "mailboxes",
  ]);
  const [pairing, setPairing] = useState<NativePairing>();
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState("");
  const trigger = useRef<HTMLButtonElement>(null);
  const wasPairing = useRef(false);
  const managementID = useId();
  const nativeID = useId();
  useEffect(() => {
    if (wasPairing.current && !pairing) trigger.current?.focus();
    wasPairing.current = !!pairing;
  }, [pairing]);
  async function create() {
    setBusy("create");
    setError(undefined);
    try {
      const value = await api<Invitation>("/device-invitations", {
        method: "POST",
        body: {
          core_origin: management ? origin : "",
          scopes: management ? scopes : [],
          management,
          native,
        },
      });
      setPairing({
        id: value.pairing_id ?? value.invitation_id,
        status: "waiting",
        device_name: "",
        expires_at: value.expires_at,
        uri: value.uri,
        fingerprint: value.fingerprint,
        managementOnly: !value.pairing_id,
      });
    } catch (error) {
      setError(error);
    } finally {
      setBusy("");
    }
  }
  return (
    <>
      <Panel>
        <div className="flex flex-col gap-6 p-5 md:p-6">
          <ErrorNotice error={error ?? devices.error} />
          <FieldGroup className="gap-6">
            <FieldGroup className="gap-6">
              <FieldSet className="gap-4">
                <FieldLegend className="sr-only">
                  {t("ui.management_scopes")}
                </FieldLegend>
                <FieldGroup className="gap-4 [&>[data-slot=field-group]]:gap-3">
                  <Field orientation="horizontal">
                    <Checkbox
                      id={managementID}
                      checked={management}
                      onCheckedChange={(value) => setManagement(value === true)}
                    />
                    <FieldLabel htmlFor={managementID}>
                      {t("ui.allow_app_management")}
                    </FieldLabel>
                  </Field>
                  {management && (
                    <div className="flex flex-col gap-5 rounded-md bg-muted/40 p-4 md:p-5">
                      <div className="grid gap-3 sm:grid-cols-[7rem_minmax(0,1fr)] sm:items-start">
                        <FieldLabel
                          htmlFor={`${managementID}-origin`}
                          className="sm:pt-2"
                        >
                          {t("ui.core_https_origin")}
                        </FieldLabel>
                        <div className="flex flex-col gap-2">
                          <Input
                            id={`${managementID}-origin`}
                            type="url"
                            value={origin}
                            aria-describedby={`${managementID}-origin-hint`}
                            onChange={(event) => setOrigin(event.target.value)}
                          />
                          <p
                            id={`${managementID}-origin-hint`}
                            className="text-sm text-muted-foreground"
                          >
                            {t("ui.app_management_endpoint_hint")}
                          </p>
                        </div>
                      </div>
                      <FieldSet className="grid gap-3 sm:grid-cols-[7rem_minmax(0,1fr)]">
                        <FieldLegend className="sr-only">
                          {t("ui.management_scopes")}
                        </FieldLegend>
                        <span
                          aria-hidden="true"
                          className="text-sm font-medium"
                        >
                          {t("ui.management_scopes")}
                        </span>
                        <div className="flex flex-wrap gap-x-6 gap-y-3">
                          {[
                            "mailboxes",
                            "folders",
                            "channels",
                            "diagnostics",
                            "content",
                          ].map((scope) => (
                            <Field
                              key={scope}
                              orientation="horizontal"
                              className="w-auto"
                            >
                              <Checkbox
                                id={`${managementID}-${scope}`}
                                checked={scopes.includes(scope)}
                                onCheckedChange={(value) =>
                                  setScopes((previous) =>
                                    value === true
                                      ? [...previous, scope].sort()
                                      : previous.filter(
                                          (item) => item !== scope,
                                        ),
                                  )
                                }
                              />
                              <div className="flex items-center gap-0.5">
                                <FieldLabel
                                  htmlFor={`${managementID}-${scope}`}
                                >
                                  {t(`ui.scope_${scope}`)}
                                </FieldLabel>
                                {scope === "content" && (
                                  <ContentPermissionInfo />
                                )}
                              </div>
                            </Field>
                          ))}
                        </div>
                      </FieldSet>
                    </div>
                  )}
                </FieldGroup>
              </FieldSet>
              <FieldSet className="gap-4 border-t border-border-subtle pt-5">
                <FieldLegend className="sr-only">
                  {t("ui.push_permission")}
                </FieldLegend>
                <FieldGroup className="gap-4 [&>[data-slot=field-group]]:gap-3">
                  <Field orientation="horizontal">
                    <Checkbox
                      id={nativeID}
                      checked={native}
                      disabled={!settings.data?.native_available}
                      onCheckedChange={(value) => setNative(value === true)}
                    />
                    <FieldLabel htmlFor={nativeID}>
                      {t("ui.enable_native_push")}
                    </FieldLabel>
                  </Field>
                </FieldGroup>
              </FieldSet>
            </FieldGroup>
          </FieldGroup>
          <div className="flex flex-col items-start gap-3">
            <BusyButton
              ref={trigger}
              className="self-start"
              busy={busy === "create"}
              disabled={
                !!busy ||
                !!pairing ||
                (!management && !native) ||
                (management && (!origin || scopes.length === 0))
              }
              onClick={() => void create()}
            >
              {t("ui.add_phone")}
            </BusyButton>
          </div>
        </div>
      </Panel>
      <AuthorizedApps
        management={devices.data?.devices}
        loading={!devices.data}
      />
      {pairing && (
        <PairingDialog
          created={pairing}
          onClose={() => setPairing(undefined)}
        />
      )}
    </>
  );
}

function AuthorizedApps({
  management,
  loading,
}: {
  management?: ManagementDevice[];
  loading: boolean;
}) {
  const { t } = useI18n();
  const client = useQueryClient();
  const settings = useQuery({
    queryKey: ["delivery-settings"],
    queryFn: () => api<DeliverySettings>("/settings/delivery"),
  });
  const push = useQuery({
    queryKey: ["native-devices"],
    queryFn: () => api<{ devices: NativePairing[] }>("/native/devices"),
    enabled: !!settings.data?.native_available,
  });
  const [confirm, setConfirm] = useState<{
    kind: "management" | "push";
    id: string;
    name: string;
  }>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const rows = new Map<
    string,
    { name: string; management?: ManagementDevice; push: NativePairing[] }
  >();
  for (const device of management ?? [])
    rows.set(device.device_id, {
      name: device.device_name,
      management: device,
      push: [],
    });
  for (const device of push.data?.devices ?? []) {
    const key = device.device_id || device.id;
    const row = rows.get(key) ?? { name: device.device_name, push: [] };
    row.push.push(device);
    rows.set(key, row);
  }
  async function revoke() {
    if (!confirm) return;
    setBusy(true);
    setError(undefined);
    try {
      await api(
        `/${confirm.kind === "management" ? "management" : "native"}/devices/${encodeURIComponent(confirm.id)}`,
        { method: "DELETE" },
      );
      await Promise.all(
        ["management-devices", "native-devices", "status"].map((key) =>
          client.invalidateQueries({ queryKey: [key] }),
        ),
      );
      setConfirm(undefined);
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Panel title={t("ui.management_devices")}>
      <div className="p-5 md:p-6">
        <ErrorNotice error={push.error} />
        {loading ? (
          <p>{t("ui.loading")}</p>
        ) : rows.size === 0 ? (
          <p className="text-sm text-muted-foreground">
            {t("ui.no_management_devices")}
          </p>
        ) : (
          <Table className="min-w-[900px] [&_th]:h-12 [&_th]:px-5 [&_td]:px-5 [&_td]:py-4">
            <TableHeader>
              <TableRow>
                <TableHead>{t("ui.phones")}</TableHead>
                <TableHead>{t("ui.management_scopes")}</TableHead>
                <TableHead>{t("ui.push_permission")}</TableHead>
                <TableHead>{t("ui.last_used")}</TableHead>
                <TableHead className="text-center">{t("ui.actions")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {Array.from(rows, ([id, row]) => (
                <TableRow key={id}>
                  <TableCell>
                    <div className="flex flex-col gap-2">
                      <strong>{row.name || t("ui.phone_unnamed")}</strong>
                      <span className="font-mono text-xs text-muted-foreground">
                        {id.slice(0, 8)}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell className="max-w-sm whitespace-normal">
                    {row.management ? (
                      <div className="flex flex-wrap items-center gap-2">
                        {row.management.scopes.map((scope) => (
                          <Badge key={scope} variant="secondary">
                            {t(`ui.scope_${scope}`)}
                          </Badge>
                        ))}
                      </div>
                    ) : (
                      <span className="text-muted-foreground">
                        {t("ui.not_authorized")}
                      </span>
                    )}
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-col gap-2 text-muted-foreground">
                      {row.push.length
                        ? row.push.map((device) => (
                            <Badge
                              key={device.id}
                              variant={
                                device.status === "active"
                                  ? "success"
                                  : "neutral"
                              }
                            >
                              {t(`ui.pairing_${device.status}`)}
                            </Badge>
                          ))
                        : t("ui.not_authorized")}
                    </div>
                  </TableCell>
                  <TableCell className="whitespace-nowrap">
                    {row.management?.last_used_at ? (
                      <Time value={row.management.last_used_at} />
                    ) : (
                      <span className="text-muted-foreground">
                        {row.management ? t("ui.never") : "—"}
                      </span>
                    )}
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center justify-center gap-2">
                      {row.management && (
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() =>
                            setConfirm({
                              kind: "management",
                              id: row.management!.controller_id,
                              name: row.name,
                            })
                          }
                        >
                          {t("ui.revoke_management")}
                        </Button>
                      )}
                      {row.push.map((device) => (
                        <Button
                          key={device.id}
                          size="sm"
                          variant="outline"
                          onClick={() =>
                            setConfirm({
                              kind: "push",
                              id: device.id,
                              name: row.name,
                            })
                          }
                        >
                          {t(
                            device.status === "revoked"
                              ? "ui.remove"
                              : "ui.revoke_push",
                          )}
                        </Button>
                      ))}
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </div>
      {confirm && (
        <ConfirmActionDialog
          confirmLabel={t("ui.revoke")}
          title={t(
            confirm.kind === "management"
              ? "ui.revoke_management"
              : "ui.revoke_push",
          )}
          description={`${confirm.name} · ${t(confirm.kind === "management" ? "ui.revoke_management_confirm" : "ui.revoke_push_confirm")}`}
          busy={busy}
          error={error}
          onConfirm={() => void revoke()}
          onClose={() => {
            setConfirm(undefined);
            setError(undefined);
          }}
        />
      )}
    </Panel>
  );
}

function ContentPermissionInfo() {
  const { t } = useI18n();
  return (
    <TooltipProvider>
      <Tooltip>
        <span className="inline-flex text-muted-foreground">
          <TooltipTrigger asChild>
            <button
              type="button"
              className="inline-flex size-5 items-center justify-center rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
              aria-label={t("ui.scope_content_hint")}
            >
              <CircleHelp className="size-3.5" aria-hidden="true" />
            </button>
          </TooltipTrigger>
        </span>
        <TooltipContent className="max-w-xs">
          {t("ui.scope_content_hint")}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
