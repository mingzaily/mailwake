import { useRef, useState } from "react";
import { CircleHelp } from "lucide-react";
import { useI18n } from "@/lib/i18n";
import { connectionUsage } from "@/lib/forms";
import type { Folder } from "@/lib/types";
import { Button } from "./ui/button";
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from "./ui/popover";

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
