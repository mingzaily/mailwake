import { useEffect, useId, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import QRCode from "qrcode";
import { api } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import type { DeliverySettings } from "@/lib/types";
import { Button } from "./ui/button";
import { Checkbox } from "./ui/checkbox";
import {
  Field,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "./ui/field";
import { Alert, AlertDescription } from "./ui/alert";
import {
  BusyButton,
  EmptyState,
  ErrorNotice,
  Panel,
  TextField,
  InlineConfirm,
  Time,
} from "./common";
export type NativePairing = {
  id: string;
  status: "waiting" | "active" | "expired" | "failed" | "revoked";
  device_name: string;
  expires_at: string;
  revoked_at?: string;
  error_code?: string;
  uri?: string;
  fingerprint?: string;
  /** Set for a management-only invitation, which has no pairing status to follow. */
  managementOnly?: boolean;
};

export function NativeDevices({
  pairingEnabled = true,
}: {
  pairingEnabled?: boolean;
}) {
  const { t, language } = useI18n();
  const client = useQueryClient();
  const settings = useQuery({
    queryKey: ["delivery-settings"],
    queryFn: () => api<DeliverySettings>("/settings/delivery"),
  });
  const devices = useQuery({
    queryKey: ["native-devices"],
    queryFn: ({ signal }) =>
      api<{ devices: NativePairing[] }>("/native/devices", { signal }),
    enabled: !!settings.data?.native_available,
  });
  const [pairing, setPairing] = useState<NativePairing>();
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState("");
  const [confirm, setConfirm] = useState("");
  const unpairTriggers = useRef(new Map<string, HTMLButtonElement>());
  const cancelUnpair = useRef<HTMLButtonElement>(null);
  const previousConfirm = useRef("");
  const confirmDescription = useId();
  useEffect(() => {
    if (confirm) cancelUnpair.current?.focus();
    else if (previousConfirm.current)
      unpairTriggers.current.get(previousConfirm.current)?.focus();
    previousConfirm.current = confirm;
  }, [confirm]);
  const trigger = useRef<HTMLButtonElement>(null);
  const wasPairing = useRef(false);
  useEffect(() => {
    if (wasPairing.current && !pairing) trigger.current?.focus();
    wasPairing.current = !!pairing;
  }, [pairing]);
  if (!settings.data?.native_available) return null;
  async function create() {
    setBusy("create");
    setError(undefined);
    try {
      setPairing(
        await api<NativePairing>("/native/pairings", { method: "POST" }),
      );
    } catch (error) {
      setError(error);
    } finally {
      setBusy("");
    }
  }
  async function remove(id: string) {
    setBusy(id);
    setError(undefined);
    try {
      await api(`/native/devices/${encodeURIComponent(id)}`, {
        method: "DELETE",
      });
      setConfirm("");
      await client.invalidateQueries({ queryKey: ["native-devices"] });
    } catch (error) {
      setError(error);
    } finally {
      setBusy("");
    }
  }
  return (
    <Panel
      title={t("ui.phones")}
      actions={
        pairingEnabled ? (
          <BusyButton
            ref={trigger}
            busy={busy === "create"}
            disabled={!!busy || !!pairing}
            onClick={() => void create()}
          >
            {t("ui.pair_phone")}
          </BusyButton>
        ) : undefined
      }
    >
      <div className="p-4 md:p-5 flex flex-col gap-4">
        <ErrorNotice error={error ?? devices.error} />
        {!devices.data ? (
          <p>{t("ui.loading")}</p>
        ) : devices.data.devices.length === 0 ? (
          <EmptyState message={t("ui.no_phones")} />
        ) : (
          <ul className="flex list-none flex-col gap-4 p-0">
            {devices.data.devices.map((device) => (
              <li
                key={device.id}
                className="flex flex-wrap items-center gap-3 justify-between"
              >
                <span>
                  {device.device_name || t("ui.phone_unnamed")}
                  {device.status === "revoked" && (
                    <>
                      {" "}
                      · {t("ui.pairing_revoked")}
                      {device.revoked_at && (
                        <>
                          {" "}
                          ·{" "}
                          <time dateTime={device.revoked_at}>
                            {new Date(device.revoked_at).toLocaleString(
                              language,
                            )}
                          </time>
                        </>
                      )}
                    </>
                  )}
                </span>
                {confirm === device.id ? (
                  <div className="flex flex-wrap items-center gap-3">
                    <span id={confirmDescription}>
                      {t(
                        device.status === "revoked"
                          ? "ui.remove_device_confirm"
                          : "ui.unpair_confirm",
                      )}
                    </span>
                    <BusyButton
                      aria-describedby={confirmDescription}
                      variant="destructive"
                      busy={busy === device.id}
                      disabled={!!busy}
                      onClick={() => void remove(device.id)}
                    >
                      {t(
                        device.status === "revoked" ? "ui.remove" : "ui.unpair",
                      )}
                    </BusyButton>
                    <Button
                      variant="outline"
                      disabled={!!busy}
                      ref={cancelUnpair}
                      aria-describedby={confirmDescription}
                      onClick={() => setConfirm("")}
                    >
                      {t("ui.cancel")}
                    </Button>
                  </div>
                ) : (
                  <Button
                    variant="outline"
                    disabled={!!busy}
                    ref={(node) => {
                      if (node) unpairTriggers.current.set(device.id, node);
                      else unpairTriggers.current.delete(device.id);
                    }}
                    onClick={() => setConfirm(device.id)}
                  >
                    {t(device.status === "revoked" ? "ui.remove" : "ui.unpair")}
                  </Button>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>
      {pairing && (
        <PairingDialog
          created={pairing}
          onClose={() => setPairing(undefined)}
        />
      )}
    </Panel>
  );
}

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
  const query = useQuery({
    queryKey: ["native-pairing", created.id],
    queryFn: ({ signal }) =>
      api<NativePairing>(`/native/pairings/${encodeURIComponent(created.id)}`, {
        signal,
      }),
    enabled: !created.managementOnly,
    refetchInterval: (query) =>
      !query.state.data || query.state.data.status === "waiting" ? 3000 : false,
    refetchIntervalInBackground: false,
  });
  // A management-only invitation is accepted directly by the App; follow the device list.
  useQuery({
    queryKey: ["management-devices"],
    queryFn: ({ signal }) =>
      api<{ devices: ManagementDevice[] }>("/management/devices", { signal }),
    enabled: !!created.managementOnly,
    refetchInterval: 3000,
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
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
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
  return (
    <dialog
      ref={dialog}
      className="m-auto max-h-[calc(100dvh-2rem)] w-[min(28rem,calc(100vw-2rem))] overflow-hidden rounded-lg border bg-card p-0 text-card-foreground backdrop:bg-[var(--dialog-backdrop)]"
      aria-labelledby={title}
      aria-describedby={description}
      onClose={onClose}
    >
      <div className="flex max-h-[calc(100dvh-2rem)] flex-col">
        <header className="shrink-0 border-b p-6">
          <h2
            ref={heading}
            tabIndex={-1}
            id={title}
            className="text-lg font-semibold"
          >
            {t("ui.pair_phone")}
          </h2>
        </header>
        <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto overscroll-contain p-6">
          <Alert>
            <AlertDescription id={description}>
              {t("ui.pairing_warning")}
            </AlertDescription>
          </Alert>
          <ErrorNotice error={query.error ?? qrError} />
          {status === "waiting" && image && (
            <img
              className="mx-auto block h-auto w-full max-w-[280px]"
              src={image}
              alt={t("ui.pairing_qr")}
              width={280}
              height={280}
            />
          )}
          <p className="text-xs text-muted-foreground">
            {t("ui.core_fingerprint")}{" "}
            <strong className="font-mono text-[13px] tabular-nums">
              {created.fingerprint}
            </strong>
          </p>
          <p role="status">
            {t(`ui.pairing_${status}`)}
            {status === "active" && current.device_name
              ? ` · ${current.device_name}`
              : ""}
          </p>
          {status === "waiting" && (
            <p className="font-mono text-[13px] tabular-nums">
              {t("ui.pairing_seconds", { seconds: String(remaining) })}
            </p>
          )}
          {current.error_code && (
            <Alert variant="destructive">
              <AlertDescription>{t(current.error_code)}</AlertDescription>
            </Alert>
          )}
        </div>
        <footer className="flex shrink-0 justify-end border-t p-4">
          <Button variant="outline" onClick={() => dialog.current?.close()}>
            {t("ui.close")}
          </Button>
        </footer>
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
  const client = useQueryClient();
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
  const [native, setNative] = useState(false);
  const [origin, setOrigin] = useState(() =>
    window.location.protocol === "https:" ? window.location.origin : "",
  );
  const [scopes, setScopes] = useState([
    "channels",
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
  async function revoke(id: string) {
    setBusy(id);
    setError(undefined);
    try {
      await api(`/management/devices/${encodeURIComponent(id)}`, {
        method: "DELETE",
      });
      await client.invalidateQueries({ queryKey: ["management-devices"] });
    } catch (error) {
      setError(error);
    } finally {
      setBusy("");
    }
  }
  return (
    <Panel title={t("ui.management_devices")}>
      <div className="flex flex-col gap-4 p-4 md:p-5">
        <ErrorNotice error={error ?? devices.error} />
        <FieldGroup>
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
          {management && (
            <>
              <TextField
                label={t("ui.core_https_origin")}
                hint={t("ui.app_management_endpoint_hint")}
                type="url"
                value={origin}
                onChange={(event) => setOrigin(event.target.value)}
              />
              <FieldSet>
                <FieldLegend>{t("ui.management_scopes")}</FieldLegend>
                <FieldGroup>
                  {["channels", "content", "diagnostics", "folders", "mailboxes"].map(
                    (scope) => (
                      <Field key={scope} orientation="horizontal">
                        <Checkbox
                          id={`${managementID}-${scope}`}
                          checked={scopes.includes(scope)}
                          onCheckedChange={(value) =>
                            setScopes((previous) =>
                              value === true
                                ? [...previous, scope].sort()
                                : previous.filter((item) => item !== scope),
                            )
                          }
                        />
                        <FieldLabel htmlFor={`${managementID}-${scope}`}>
                          {t(`ui.scope_${scope}`)}
                        </FieldLabel>
                      </Field>
                    ),
                  )}
                </FieldGroup>
              </FieldSet>
            </>
          )}
        </FieldGroup>
        <p className="text-sm text-muted-foreground">
          {t("ui.app_management_pro_hint")}
        </p>
        <BusyButton
          ref={trigger}
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
        {!devices.data ? (
          <p>{t("ui.loading")}</p>
        ) : devices.data.devices.length === 0 ? (
          <EmptyState message={t("ui.no_management_devices")} />
        ) : (
          <ul className="flex list-none flex-col gap-4 p-0">
            {devices.data.devices.map((device) => (
              <li
                key={device.controller_id}
                className="flex flex-wrap items-center justify-between gap-3"
              >
                <div>
                  <strong>{device.device_name || t("ui.phone_unnamed")}</strong>
                  <p className="text-sm text-muted-foreground">
                    {device.scopes
                      .map((scope) => t(`ui.scope_${scope}`))
                      .join(" · ")}{" "}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    {t("ui.last_used")}{" "}
                    {device.last_used_at ? (
                      <Time value={device.last_used_at} />
                    ) : (
                      t("ui.never")
                    )}
                  </p>
                </div>
                <InlineConfirm
                  label={t("ui.revoke")}
                  question={t("ui.revoke_management_confirm")}
                  confirm={t("ui.revoke")}
                  busy={busy === device.controller_id}
                  onConfirm={() => revoke(device.controller_id)}
                />
              </li>
            ))}
          </ul>
        )}
        {pairing && (
          <PairingDialog
            created={pairing}
            onClose={() => setPairing(undefined)}
          />
        )}
      </div>
    </Panel>
  );
}
