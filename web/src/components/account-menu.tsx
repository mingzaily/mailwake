import { useRef, useState } from "react";
import { ChevronsUpDown, LogOut, Settings } from "lucide-react";
// Avatar from the shadcn sidebar-08 example (web/SHADCN-LICENSE).
import defaultAvatar from "@/assets/default-avatar.jpg";
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
              <img
                src={defaultAvatar}
                alt=""
                width={32}
                height={32}
                className="size-8 shrink-0 rounded-lg object-cover"
              />
              <span className="grid min-w-0 flex-1 gap-0.5 text-left leading-tight">
                <span className="truncate font-medium" title={username}>
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
