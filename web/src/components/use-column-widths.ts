import { useRef, useState, type PointerEvent, type KeyboardEvent } from "react";

// Capture rendered widths before resizing so wide containers never cause a jump.
export function useColumnWidths(initial: number[], minimum: number[]) {
  const ref = useRef<HTMLTableElement>(null);
  const [widths, setWidths] = useState(initial);
  const drag = useRef<{ index: number; x: number; widths: number[] } | null>(
    null,
  );
  const rendered = () =>
    Array.from(
      ref.current!.querySelectorAll("thead th"),
      (cell) => cell.getBoundingClientRect().width,
    );
  function resize(values: number[], index: number, delta: number) {
    setWidths(
      values.map((value, i) =>
        i === index
          ? Math.max(minimum[index], Math.min(800, value + delta))
          : value,
      ),
    );
  }
  return {
    ref,
    widths,
    width: widths.reduce((total, width) => total + width, 0),
    handle: (index: number) => ({
      onPointerDown(event: PointerEvent<HTMLButtonElement>) {
        if (event.button !== 0) return;
        event.preventDefault();
        event.currentTarget.focus();
        event.currentTarget.setPointerCapture(event.pointerId);
        drag.current = { index, x: event.clientX, widths: rendered() };
      },
      onPointerMove(event: PointerEvent<HTMLButtonElement>) {
        if (drag.current?.index === index)
          resize(drag.current.widths, index, event.clientX - drag.current.x);
      },
      onPointerUp() {
        drag.current = null;
      },
      onLostPointerCapture() {
        drag.current = null;
      },
      onKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
        if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
        event.preventDefault();
        resize(rendered(), index, event.key === "ArrowRight" ? 16 : -16);
      },
    }),
  };
}
