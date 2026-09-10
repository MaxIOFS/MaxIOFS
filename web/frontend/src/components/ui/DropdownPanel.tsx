import React, { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';

export interface DropdownPanelProps {
  /** The element the panel hangs off. Its box decides where the panel goes. */
  anchorRef: React.RefObject<HTMLElement | null>;
  open: boolean;
  onClose: () => void;
  /** Panel width in pixels. */
  width?: number;
  children: React.ReactNode;
}

const MARGIN = 8;

/**
 * A menu panel rendered into document.body. The page's scroll container clips
 * anything positioned inside it, so a panel that lives there cannot open past
 * the top of the list or over the bar above it, whatever its z-index.
 */
export function DropdownPanel({ anchorRef, open, onClose, width = 224, children }: DropdownPanelProps) {
  const panelRef = useRef<HTMLDivElement>(null);
  const [style, setStyle] = useState<React.CSSProperties>({ visibility: 'hidden' });

  useLayoutEffect(() => {
    if (!open) return;

    const place = () => {
      const anchor = anchorRef.current;
      if (!anchor) return;

      const rect = anchor.getBoundingClientRect();
      const height = panelRef.current?.offsetHeight ?? 0;
      const below = window.innerHeight - rect.bottom;

      // Flip up only when there is room above and not below.
      const dropUp = below < height + MARGIN && rect.top > height + MARGIN;
      const top = dropUp ? rect.top - height - 4 : rect.bottom + 4;

      let left = rect.right - width;
      left = Math.max(MARGIN, Math.min(left, window.innerWidth - width - MARGIN));

      setStyle({
        position: 'fixed',
        top: Math.max(MARGIN, top),
        left,
        width,
        maxHeight: dropUp ? rect.top - MARGIN * 2 : window.innerHeight - rect.bottom - MARGIN * 2,
        overflowY: 'auto',
        visibility: 'visible',
      });
    };

    place();
    // A fixed panel does not travel with the page, so a scroll of the page
    // closes it — but scrolling the panel's own list must not.
    const dismiss = (e: Event) => {
      if (panelRef.current?.contains(e.target as Node)) return;
      onClose();
    };
    window.addEventListener('scroll', dismiss, true);
    window.addEventListener('resize', place);
    return () => {
      window.removeEventListener('scroll', dismiss, true);
      window.removeEventListener('resize', place);
    };
  }, [open, anchorRef, width, onClose]);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (e: MouseEvent) => {
      const target = e.target as Node;
      if (panelRef.current?.contains(target)) return;
      if (anchorRef.current?.contains(target)) return;
      onClose();
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('mousedown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('mousedown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [open, anchorRef, onClose]);

  if (!open) return null;

  return createPortal(
    <div
      ref={panelRef}
      role="menu"
      style={style}
      className="z-[100] rounded-md border border-border bg-card shadow-lg"
    >
      {children}
    </div>,
    document.body,
  );
}
