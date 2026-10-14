import { AccountMenu } from "./account-menu";
import { useRef, useState, type ReactNode } from "react";
import {
  Activity,
  Bell,
  FileText,
  Gauge,
  CodeXml,
  PanelLeft,
  Mail,
  Settings,
  ShieldCheck,
  Smartphone,
  X,
} from "lucide-react";
import { useI18n } from "@/lib/i18n";
import { useMailboxes, useStatus } from "@/lib/queries";
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
} from "./ui/sidebar";
import {
  Breadcrumb,
  BreadcrumbList,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "./ui/breadcrumb";
import { Separator } from "./ui/separator";
import { cn } from "@/lib/utils";
import { Button } from "./ui/button";
import { Brand } from "./brand";
import { ErrorNotice } from "./common";

const pages = [
  { id: "overview", icon: Gauge },
  { id: "mailboxes", icon: Mail },
  { id: "app_settings", icon: Smartphone },
  { id: "notifications", icon: Bell },
  { id: "deliveries", icon: Activity },
  { id: "logs", icon: FileText },
  { id: "settings", icon: Settings },
  { id: "diagnostics", icon: ShieldCheck },
];
export function Shell({
  page,
  mailboxId,
  children,
  logout,
  username,
}: {
  page: string;
  username?: string;
  mailboxId?: string;
  children: ReactNode;
  logout: () => void;
}) {
  const { t } = useI18n();
  const boxes = useMailboxes();
  const status = useStatus();
  const dialog = useRef<HTMLDialogElement>(null);
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const selected =
    page === "mailboxes"
      ? boxes.data?.mailboxes.find((box) => box.id === mailboxId)
      : undefined;
  const rail = (
    <>
      <SidebarHeader className="p-4">
        <Brand />
      </SidebarHeader>
      <SidebarContent className="px-3 py-2">
        <nav aria-label={t("ui.navigation")}>
          <SidebarMenu className="gap-1">
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
      <SidebarFooter className="gap-1 px-3 pt-2 pb-3">
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton asChild>
              <a
                href="https://github.com/mingzaily/mailwake"
                target="_blank"
                rel="noopener noreferrer"
              >
                <CodeXml aria-hidden="true" />
                <span>{t("ui.github_repository")}</span>
              </a>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
        <AccountMenu username={username} logout={logout} />
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
        className={cn(
          "sticky top-0 hidden h-svh shrink-0 border-r",
          sidebarOpen && "md:flex",
        )}
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
            <PanelLeft aria-hidden="true" />
          </Button>
          <Button
            className="hidden md:inline-flex"
            variant="ghost"
            size="icon-sm"
            aria-label={t("ui.toggle_sidebar")}
            aria-expanded={sidebarOpen}
            onClick={() => setSidebarOpen((open) => !open)}
          >
            <PanelLeft aria-hidden="true" />
          </Button>
          <Separator
            orientation="vertical"
            className="data-[orientation=vertical]:h-4"
          />
          <Breadcrumb
            aria-label={t("ui.breadcrumb")}
            className="min-w-0 flex-1"
          >
            <BreadcrumbList className="flex-nowrap">
              <BreadcrumbItem className="shrink-0">
                <BreadcrumbLink href="#/overview">
                  {t("ui.brand")}
                </BreadcrumbLink>
              </BreadcrumbItem>
              <BreadcrumbSeparator />
              <BreadcrumbItem className="min-w-0">
                {selected || (page === "mailboxes" && mailboxId === "new") ? (
                  <BreadcrumbLink href="#/mailboxes">
                    {t("ui.mailboxes")}
                  </BreadcrumbLink>
                ) : (
                  <BreadcrumbPage className="truncate">
                    {t(`ui.${page}`)}
                  </BreadcrumbPage>
                )}
              </BreadcrumbItem>
              {(selected || (page === "mailboxes" && mailboxId === "new")) && (
                <>
                  <BreadcrumbSeparator />
                  <BreadcrumbItem className="min-w-0">
                    <BreadcrumbPage
                      className="truncate"
                      title={selected?.label}
                    >
                      {selected?.label ?? t("ui.add_mailbox")}
                    </BreadcrumbPage>
                  </BreadcrumbItem>
                </>
              )}
            </BreadcrumbList>
          </Breadcrumb>
          <div className="hidden shrink-0 items-center gap-4 text-sm font-normal text-muted-foreground min-[1100px]:flex">
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
                <span className="text-foreground tabular-nums">
                  {count ?? "—"}
                </span>
              </span>
            ))}
          </div>
        </header>
        <main
          id="main-content"
          className="mx-auto grid w-full max-w-[1440px] grid-cols-1 content-start gap-5 px-4 pt-6 pb-9 md:px-7 md:pb-12"
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
