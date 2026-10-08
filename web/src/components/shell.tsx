import { useRef, useState, type ReactNode } from "react";
import {
  Activity,
  Bell,
  CircleHelp,
  FileText,
  Gauge,
  Mail,
  Menu,
  Settings,
  ShieldCheck,
  X,
} from "lucide-react";
import { useI18n } from "@/lib/i18n";
import { useMailboxes, useStatus } from "@/lib/queries";
import { connectionUsage } from "@/lib/forms";
import {
  Sidebar,
  SidebarProvider,
  SidebarHeader,
  SidebarContent,
  SidebarFooter,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
  SidebarMenuBadge,
  SidebarMenuSub,
  SidebarMenuSubItem,
  SidebarMenuSubButton,
  SidebarSeparator,
} from "./ui/sidebar";
import { Button } from "./ui/button";
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from "./ui/popover";
import type { Folder } from "@/lib/types";
import { ErrorNotice } from "./common";

const pages = [
  { id: "overview", icon: Gauge },
  { id: "mailboxes", icon: Mail },
  { id: "notifications", icon: Bell },
  { id: "deliveries", icon: Activity },
  { id: "logs", icon: FileText },
  { id: "settings", icon: Settings },
  { id: "diagnostics", icon: ShieldCheck },
];
export function Brand() {
  const { t } = useI18n();
  return (
    <div className="flex items-center gap-2.5 font-semibold">
      <Mail aria-hidden="true" className="size-6 text-primary" />
      <b translate="no">{t("ui.brand")}</b>
      <small className="ml-auto font-mono text-xs font-normal text-muted-foreground">
        {t("ui.core")}
      </small>
    </div>
  );
}
export function Budget({
  folders,
  limit,
}: {
  folders: Folder[];
  limit: number;
}) {
  const { t } = useI18n();
  const used = connectionUsage(folders);
  const trigger = useRef<HTMLButtonElement>(null);
  const [container, setContainer] = useState<HTMLElement | null>(null);
  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-2 text-xs">
        <span className="flex items-center gap-1">
          {t("ui.budget")}
          <Popover
            onOpenChange={(open) => {
              // The folder scan dialog is a native modal; the popover must render inside it to stay visible.
              if (open)
                setContainer(trigger.current?.closest("dialog") ?? null);
            }}
          >
            <PopoverTrigger asChild>
              <Button
                ref={trigger}
                type="button"
                variant="ghost"
                size="icon-xs"
                aria-label={t("ui.budget_help_label")}
              >
                <CircleHelp aria-hidden="true" />
              </Button>
            </PopoverTrigger>
            <PopoverContent
              container={container}
              align="start"
              className="w-80"
            >
              <PopoverHeader>
                <PopoverTitle>{t("ui.budget")}</PopoverTitle>
                <PopoverDescription>{t("ui.budget_help")}</PopoverDescription>
              </PopoverHeader>
            </PopoverContent>
          </Popover>
        </span>
        <span className="font-mono text-[13px] tabular-nums">
          {used} / {limit - 1}
        </span>
      </div>
      <div className="flex flex-wrap gap-0.5" aria-hidden="true">
        {Array.from({ length: limit }, (_, i) => (
          <i
            key={i}
            className={
              "h-1.5 min-w-0.5 flex-1 rounded-sm " +
              (i === limit - 1
                ? "border border-dashed border-muted-foreground"
                : i < used
                  ? "bg-primary"
                  : "bg-border")
            }
          />
        ))}
      </div>
      <p className="text-muted-foreground text-xs">
        {t("ui.budget_hint", { used, limit })}
      </p>
    </div>
  );
}
export function Shell({
  page,
  mailboxId,
  children,
  logout,
}: {
  page: string;
  mailboxId?: string;
  children: ReactNode;
  logout: () => void;
}) {
  const { t } = useI18n();
  const boxes = useMailboxes();
  const status = useStatus();
  const dialog = useRef<HTMLDialogElement>(null);
  const selected =
    page === "mailboxes"
      ? boxes.data?.mailboxes.find((box) => box.id === mailboxId)
      : undefined;
  const folders =
    status.data?.folders
      .filter((folder) => folder.mailbox_id === selected?.id)
      .map((folder) => ({ name: folder.folder, check: folder.check })) ?? [];
  const rail = (
    <>
      <SidebarHeader className="p-4">
        <Brand />
      </SidebarHeader>
      <SidebarContent className="px-3 py-2">
        <nav aria-label={t("ui.navigation")}>
          <SidebarMenu className="gap-2">
            {pages.map(({ id, icon: Icon }) => (
              <SidebarMenuItem key={id}>
                <SidebarMenuButton
                  asChild
                  isActive={page === id && (id !== "mailboxes" || !mailboxId)}
                >
                  <a
                    href={`#/${id}`}
                    aria-current={
                      page === id && (id !== "mailboxes" || !mailboxId)
                        ? "page"
                        : undefined
                    }
                  >
                    <Icon aria-hidden="true" />
                    <span>{t(`ui.${id}`)}</span>
                  </a>
                </SidebarMenuButton>
                {id === "mailboxes" && (
                  <SidebarMenuBadge>
                    {boxes.data?.mailboxes.length ?? 0}
                  </SidebarMenuBadge>
                )}
                {id === "mailboxes" && !!boxes.data?.mailboxes.length && (
                  <SidebarMenuSub className="mt-2 mr-0 pr-0">
                    {boxes.data.mailboxes.map((box) => (
                      <SidebarMenuSubItem key={box.id}>
                        <SidebarMenuSubButton
                          href={`#/mailboxes/${box.id}`}
                          isActive={
                            page === "mailboxes" && mailboxId === box.id
                          }
                          aria-current={
                            page === "mailboxes" && mailboxId === box.id
                              ? "page"
                              : undefined
                          }
                        >
                          <span title={box.label}>{box.label}</span>
                        </SidebarMenuSubButton>
                      </SidebarMenuSubItem>
                    ))}
                  </SidebarMenuSub>
                )}
              </SidebarMenuItem>
            ))}
          </SidebarMenu>
        </nav>
      </SidebarContent>
      <SidebarSeparator className="mx-4 data-[orientation=horizontal]:w-auto" />
      <SidebarFooter className="gap-4 p-4">
        {selected && (
          <div className="flex min-w-0 flex-col gap-2">
            <p className="truncate text-xs font-medium" title={selected.label}>
              {selected.label}
            </p>
            <Budget folders={folders} limit={selected.connection_limit} />
          </div>
        )}
        <Button variant="ghost" onClick={logout}>
          {t("ui.logout")}
        </Button>
      </SidebarFooter>
    </>
  );
  return (
    <SidebarProvider keyboardShortcut={false}>
      <a
        className="fixed top-2 left-2 z-50 -translate-y-[200%] bg-card p-2 focus:translate-y-0"
        href="#main-content"
        onClick={(event) => {
          event.preventDefault();
          document.getElementById("main-content")?.focus();
        }}
      >
        {t("ui.skip_content")}
      </a>
      <Sidebar
        collapsible="none"
        className="sticky top-0 hidden h-svh shrink-0 border-r md:flex"
      >
        {rail}
      </Sidebar>
      <dialog
        className="m-0 h-dvh max-h-dvh w-72 max-w-[85vw] overscroll-contain border-0 bg-sidebar p-0 text-sidebar-foreground backdrop:bg-[var(--drawer-backdrop)]"
        ref={dialog}
        aria-label={t("ui.navigation")}
        onClick={(event) => {
          if ((event.target as Element).closest("a")) dialog.current?.close();
        }}
      >
        <div className="flex justify-end p-2">
          <Button
            variant="ghost"
            size="icon"
            aria-label={t("ui.close")}
            onClick={() => dialog.current?.close()}
          >
            <X aria-hidden="true" />
          </Button>
        </div>
        <Sidebar collapsible="none" className="h-[calc(100%-3.25rem)] w-full">
          {rail}
        </Sidebar>
      </dialog>
      <div className="min-w-0 flex-1">
        <header className="sticky top-0 z-10 flex h-12 items-center gap-2.5 border-b bg-background px-4 md:gap-4 md:px-6">
          <Button
            className="md:hidden"
            variant="ghost"
            size="icon-sm"
            aria-label={t("ui.menu")}
            onClick={() => dialog.current?.showModal()}
          >
            <Menu aria-hidden="true" />
          </Button>
          <div className="hidden flex-1 items-center gap-4 text-xs text-muted-foreground min-[900px]:flex">
            {[
              [t("ui.mailboxes"), boxes.data?.mailboxes.length],
              [
                t("ui.active_folders"),
                status.data?.folders.filter((folder) =>
                  ["watching", "idle", "poll", "scheduled"].includes(
                    folder.state,
                  ),
                ).length,
              ],
              [t("ui.pending"), status.data?.delivery.pending],
              [t("ui.failed"), status.data?.delivery.dead],
            ].map(([label, count]) => (
              <span key={label} title={String(label)}>
                <span>{label} </span>
                <b className="font-mono font-medium text-foreground tabular-nums">
                  {count ?? "—"}
                </b>
              </span>
            ))}
          </div>
          <span className="text-muted-foreground text-xs">
            {t(`ui.${page}`)}
          </span>
        </header>
        <main
          id="main-content"
          className="mx-auto grid w-full max-w-[1440px] content-start gap-5 px-4 pt-6 pb-9 md:px-7 md:pb-12"
          tabIndex={-1}
        >
          <ErrorNotice
            error={boxes.error ?? status.error}
            reload={() => {
              void boxes.refetch();
              void status.refetch();
            }}
          />
          {children}
        </main>
      </div>
    </SidebarProvider>
  );
}
