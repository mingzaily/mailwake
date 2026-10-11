import { useEffect, useState, type ReactNode } from "react";
import {
  CircleCheckIcon,
  CircleXIcon,
  InfoIcon,
  Loader2Icon,
  TriangleAlertIcon,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { Toaster as Sonner, type ToasterProps } from "sonner";

// shadcn Sonner composition, using Core's document theme instead of next-themes.
export function ToastMessage({
  children,
  duration,
}: {
  children: ReactNode;
  duration: number;
}) {
  return (
    <>
      {children}
      <span
        aria-hidden="true"
        className="toast-countdown"
        style={{ animationDuration: `${duration}ms` }}
      />
    </>
  );
}

export function Toaster({ className, ...props }: ToasterProps) {
  const [hidden, setHidden] = useState(document.hidden);
  useEffect(() => {
    const update = () => setHidden(document.hidden);
    document.addEventListener("visibilitychange", update);
    return () => document.removeEventListener("visibilitychange", update);
  }, []);
  const [theme, setTheme] = useState<"light" | "dark">(() =>
    document.documentElement.dataset.theme === "light" ? "light" : "dark",
  );
  useEffect(() => {
    const observer = new MutationObserver(() =>
      setTheme(
        document.documentElement.dataset.theme === "light" ? "light" : "dark",
      ),
    );
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme"],
    });
    return () => observer.disconnect();
  }, []);
  return (
    <Sonner
      theme={theme}
      className={cn("toaster group", hidden && "toast-paused", className)}
      icons={{
        success: <CircleCheckIcon className="size-4" />,
        info: <InfoIcon className="size-4" />,
        warning: <TriangleAlertIcon className="size-4" />,
        error: <CircleXIcon className="size-4" />,
        loading: <Loader2Icon className="size-4 animate-spin" />,
      }}
      {...props}
    />
  );
}
