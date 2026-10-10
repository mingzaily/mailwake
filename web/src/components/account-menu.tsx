import { useRef, useState } from "react";
import { ChevronsUpDown, LogOut, Settings, UserRound } from "lucide-react";
import { useI18n } from "@/lib/i18n";
import { SidebarMenu, SidebarMenuItem, SidebarMenuButton } from "./ui/sidebar";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuGroup,
  DropdownMenuItem,
} from "./ui/dropdown-menu";

export function AccountMenu({
  username,
  logout,
}: {
  username?: string;
  logout: () => void;
}) {
  const { t } = useI18n();
  const trigger = useRef<HTMLButtonElement>(null);
  const [container, setContainer] = useState<HTMLElement | null>(null);
  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu
          modal={false}
          onOpenChange={(open) => {
            if (open) setContainer(trigger.current?.closest("dialog") ?? null);
          }}
        >
          <DropdownMenuTrigger asChild>
            <SidebarMenuButton
              ref={trigger}
              size="lg"
              aria-label={t("ui.account_menu")}
            >
              <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-sidebar-accent">
                <UserRound aria-hidden="true" className="size-5" />
              </span>
              <span className="grid min-w-0 flex-1 gap-0.5 text-left">
                <span className="truncate font-semibold" title={username}>
                  {username ?? t("ui.administrator")}
                </span>
                <span className="text-xs text-muted-foreground">
                  {t("ui.administrator")}
                </span>
              </span>
              <ChevronsUpDown aria-hidden="true" className="ml-auto" />
            </SidebarMenuButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            container={container}
            side="top"
            align="start"
            className="w-56"
          >
            <DropdownMenuLabel className="truncate">
              {username ?? t("ui.administrator")}
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuItem asChild>
                <a href="#/settings">
                  <Settings aria-hidden="true" />
                  {t("ui.settings")}
                </a>
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={logout}>
                <LogOut aria-hidden="true" />
                {t("ui.logout")}
              </DropdownMenuItem>
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
