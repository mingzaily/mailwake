import {
  createContext,
  useContext,
  useEffect,
  useId,
  useRef,
  useState,
  type ComponentProps,
  type ReactNode,
} from "react";
import { X } from "lucide-react";
import { APIError } from "@/lib/api";
import { useI18n } from "@/lib/i18n";
import { Card, CardHeader, CardTitle, CardAction } from "./ui/card";
import { Separator } from "./ui/separator";
import { Button } from "./ui/button";
import { NativeSelect } from "./ui/native-select";
import { Spinner } from "./ui/spinner";
import { Input } from "./ui/input";
import { Alert, AlertDescription } from "./ui/alert";
import { Field, FieldDescription, FieldError, FieldLabel } from "./ui/field";
import { Empty, EmptyHeader, EmptyDescription } from "./ui/empty";

export function Panel({
  title,
  actions,
  children,
}: {
  title?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <Card className="min-w-0 gap-0 overflow-hidden py-0">
      {(title || actions) && (
        <>
          <CardHeader className="flex flex-row items-center gap-3 px-4 py-3">
            {title && (
              <CardTitle className="text-sm">
                <h2>{title}</h2>
              </CardTitle>
            )}
            {actions && (
              <CardAction className="ml-auto flex flex-wrap items-center gap-2 self-center">
                {actions}
              </CardAction>
            )}
          </CardHeader>
          <Separator className="bg-border-subtle" />
        </>
      )}
      {children}
    </Card>
  );
}
export function PageHeading({
  title,
  description,
  actions,
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
}) {
  return (
    <header className="flex flex-wrap items-end gap-4">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
        {description && (
          <p className="mt-1 text-muted-foreground">{description}</p>
        )}
      </div>
      {actions && (
        <div className="flex flex-wrap items-center gap-2 md:ml-auto">
          {actions}
        </div>
      )}
    </header>
  );
}
export function TextField({
  label,
  error,
  hint,
  ...props
}: ComponentProps<typeof Input> & {
  label: string;
  error?: string;
  hint?: string;
}) {
  const id = useId();
  return (
    <Field data-invalid={!!error}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        aria-invalid={!!error}
        aria-describedby={
          error ? `${id}-error` : hint ? `${id}-hint` : undefined
        }
        {...props}
      />
      {hint && <FieldDescription id={`${id}-hint`}>{hint}</FieldDescription>}
      {error && <FieldError id={`${id}-error`}>{error}</FieldError>}
    </Field>
  );
}
export function SelectField({
  label,
  children,
  fieldClassName,
  ...props
}: ComponentProps<typeof NativeSelect> & {
  label: string;
  fieldClassName?: string;
}) {
  const id = useId();
  return (
    <Field className={fieldClassName}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <NativeSelect id={id} {...props}>
        {children}
      </NativeSelect>
    </Field>
  );
}
export function ErrorNotice({
  error,
  reload,
}: {
  error: unknown;
  reload?: () => void;
}) {
  const { t } = useI18n();
  if (!error) return null;
  return (
    <Alert variant="destructive" role="alert">
      <AlertDescription>
        <p>
          {error instanceof APIError &&
          error.failure.params?.reason !== "invalid_response"
            ? error.message
            : t("ui.core_unreachable")}
        </p>
        {error instanceof APIError && error.status === 429 && (
          <p>{t("ui.wait_seconds", { seconds: error.retryAfter })}</p>
        )}
        {error instanceof APIError && error.conflict && (
          <>
            <p>{t("ui.conflict")}</p>
            {reload && (
              <Button type="button" variant="outline" onClick={reload}>
                {t("ui.reload")}
              </Button>
            )}
          </>
        )}
      </AlertDescription>
    </Alert>
  );
}
export function BusyButton({
  busy,
  children,
  ...props
}: ComponentProps<typeof Button> & { busy: boolean }) {
  return (
    <Button {...props} disabled={busy || props.disabled} aria-busy={busy}>
      {busy && <Spinner data-icon="inline-start" />}
      {children}
    </Button>
  );
}
export function EmptyState({ message }: { message?: string }) {
  const { t } = useI18n();
  return (
    <Empty>
      <EmptyHeader>
        <EmptyDescription>{message ?? t("ui.empty")}</EmptyDescription>
      </EmptyHeader>
    </Empty>
  );
}
export function Time({ value }: { value?: string | null }) {
  const { language } = useI18n();
  if (value == null || value === "")
    return <span className="text-muted-foreground">—</span>;
  if (typeof value !== "string")
    throw new TypeError("Time requires an ISO timestamp string");
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return <span>—</span>;
  return (
    <time
      className="font-mono text-[13px] tabular-nums"
      dateTime={date.toISOString()}
      title={date.toLocaleString(language)}
    >
      {new Intl.DateTimeFormat(language, {
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
        hour12: false,
      }).format(date)}
    </time>
  );
}
const ToastContext = createContext<(message: string) => void>(() => {});
export const useToast = () => useContext(ToastContext);
export function ToastProvider({ children }: { children: ReactNode }) {
  const [message, setMessage] = useState("");
  const { t } = useI18n();
  useEffect(() => {
    if (!message) return;
    const timer = setTimeout(() => setMessage(""), 5000);
    return () => clearTimeout(timer);
  }, [message]);
  return (
    <ToastContext.Provider value={setMessage}>
      {children}
      <div
        className="fixed right-5 bottom-[calc(20px+env(safe-area-inset-bottom,0px))] z-50 max-w-[calc(100%-40px)]"
        role="status"
        aria-live="polite"
      >
        {message && (
          <Alert role="presentation" className="flex items-center gap-3 py-2.5">
            <AlertDescription>{message}</AlertDescription>
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label={t("ui.close")}
              onClick={() => setMessage("")}
            >
              <X aria-hidden="true" />
            </Button>
          </Alert>
        )}
      </div>
    </ToastContext.Provider>
  );
}
export function InlineConfirm({
  label,
  question,
  confirm,
  onConfirm,
  busy,
}: {
  label: string;
  question: string;
  confirm: string;
  onConfirm: () => Promise<void>;
  busy: boolean;
}) {
  const [open, setOpen] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const cancel = useRef<HTMLButtonElement>(null);
  const wasOpen = useRef(false);
  const description = useId();
  useEffect(() => {
    if (open) cancel.current?.focus();
    else if (wasOpen.current) trigger.current?.focus();
    wasOpen.current = open;
  }, [open]);
  const { t } = useI18n();
  return open ? (
    <div className="flex flex-wrap items-center gap-2 whitespace-normal">
      <span id={description} className="max-w-90 text-muted-foreground">
        {question}
      </span>
      <BusyButton
        aria-describedby={description}
        variant="destructive"
        busy={busy}
        onClick={() => void onConfirm()}
      >
        {confirm}
      </BusyButton>
      <Button
        ref={cancel}
        aria-describedby={description}
        variant="ghost"
        disabled={busy}
        onClick={() => setOpen(false)}
      >
        {t("ui.cancel")}
      </Button>
    </div>
  ) : (
    <Button ref={trigger} variant="ghost" onClick={() => setOpen(true)}>
      {label}
    </Button>
  );
}
