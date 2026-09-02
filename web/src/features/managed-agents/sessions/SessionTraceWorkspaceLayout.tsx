import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
  type ResizablePanelHandle,
} from '../../../shared/ui/resizable';
import clsx from 'clsx';
import { type ReactNode, useLayoutEffect, useRef, useState } from 'react';

export const sessionTraceDualPaneMinWidth = 1056;
export const sessionTraceInspectorMinWidth = 360;
export const sessionTraceInspectorDefaultWidth = 480;
export const sessionTraceInspectorMaxViewportRatio = 0.7;
export const sessionTracePrimaryMinWidth = 360;
const sessionTraceDividerWidth = 1;

export function sessionTraceInspectorMaximumWidth(containerWidth: number, viewportWidth: number) {
  if (containerWidth <= 0 || viewportWidth <= 0) {
    return sessionTraceInspectorDefaultWidth;
  }
  return Math.max(
    sessionTraceInspectorMinWidth,
    Math.min(
      viewportWidth * sessionTraceInspectorMaxViewportRatio,
      containerWidth - sessionTracePrimaryMinWidth - sessionTraceDividerWidth,
    ),
  );
}

export function clampSessionTraceInspectorWidth(width: number, containerWidth: number, viewportWidth = containerWidth) {
  const maximumWidth = Math.round(sessionTraceInspectorMaximumWidth(containerWidth, viewportWidth));
  const requestedWidth = Number.isFinite(width) ? Math.round(width) : maximumWidth;
  return Math.min(Math.max(requestedWidth, sessionTraceInspectorMinWidth), maximumWidth);
}

export function SessionTraceWorkspaceLayout({
  primary,
  inspector,
  inspectorOpen,
  onInspectorCollapse,
  resizeLabel,
}: {
  primary: ReactNode;
  inspector: ReactNode;
  inspectorOpen: boolean;
  onInspectorCollapse?: () => void;
  resizeLabel: string;
}) {
  const layoutHostRef = useRef<HTMLDivElement | null>(null);
  const [containerWidth, setContainerWidth] = useState(0);
  const [viewportWidth, setViewportWidth] = useState(0);
  const [inspectorWidth, setInspectorWidth] = useState(sessionTraceInspectorDefaultWidth);
  const inspectorPanelRef = useRef<ResizablePanelHandle | null>(null);
  const userResizingInspectorRef = useRef(false);
  // Keep the primary pane interactive until the layout host has a measured
  // width. In browsers this state lasts only until the first layout effect;
  // treating an unknown `0px` as compact would briefly inert the transcript
  // and steal focus before the real container breakpoint is known.
  const compact = containerWidth > 0 && containerWidth < sessionTraceDualPaneMinWidth;
  const compactInspectorOpen = compact && inspectorOpen;
  const splitInspectorOpen = inspectorOpen && !compact;
  const maximumInspectorWidth = Math.round(sessionTraceInspectorMaximumWidth(containerWidth, viewportWidth));
  const inspectorViewportClassName = sessionTraceInspectorViewportClassName(compact, inspectorOpen);

  useLayoutEffect(() => {
    const container = layoutHostRef.current;
    if (!container) {
      return;
    }
    const updateWidth = () => {
      setContainerWidth(container.getBoundingClientRect().width);
      setViewportWidth(document.documentElement.clientWidth || window.innerWidth);
    };
    updateWidth();
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(updateWidth);
    observer?.observe(container);
    window.addEventListener('resize', updateWidth);
    return () => {
      observer?.disconnect();
      window.removeEventListener('resize', updateWidth);
    };
  }, []);

  useLayoutEffect(() => {
    const panel = inspectorPanelRef.current;
    if (!panel) {
      return;
    }
    if (!splitInspectorOpen) {
      panel.collapse();
      return;
    }
    const nextWidth = clampSessionTraceInspectorWidth(inspectorWidth, containerWidth, viewportWidth);
    if (nextWidth !== inspectorWidth) {
      setInspectorWidth(nextWidth);
    }
    panel.expand();
    panel.resize(nextWidth);
  }, [containerWidth, inspectorWidth, splitInspectorOpen, viewportWidth]);

  const finishUserInspectorResize = () => {
    userResizingInspectorRef.current = false;
    if (inspectorPanelRef.current?.isCollapsed()) onInspectorCollapse?.();
  };

  return (
    <div ref={layoutHostRef} className="min-h-0 min-w-0 flex-1 overflow-hidden" data-testid="session-trace-layout-host">
      <ResizablePanelGroup
        id="session-trace-resizable-panels"
        orientation="horizontal"
        className="group/session-trace-layout session-trace-layout h-full min-h-0 min-w-0"
        data-layout-mode={compact ? 'overlay' : 'split'}
        data-inspector-width={Math.round(inspectorWidth)}
      >
        <ResizablePanel
          id="session-trace-primary"
          minSize={compact ? 0 : sessionTracePrimaryMinWidth}
          className="min-w-0"
          style={{ overflow: 'hidden' }}
        >
          <div
            aria-hidden={compactInspectorOpen || undefined}
            inert={compactInspectorOpen}
            className="h-full min-h-0 min-w-0 overflow-hidden"
            data-session-trace-primary-viewport=""
          >
            {primary}
          </div>
        </ResizablePanel>
        <ResizableHandle
          aria-label={resizeLabel}
          disabled={!splitInspectorOpen}
          hidden={!splitInspectorOpen}
          className="session-trace-resize-handle cursor-col-resize bg-transparent"
          onPointerDown={() => {
            userResizingInspectorRef.current = true;
          }}
          onPointerUp={finishUserInspectorResize}
          onLostPointerCapture={finishUserInspectorResize}
          onKeyDown={() => {
            userResizingInspectorRef.current = true;
          }}
          onKeyUp={finishUserInspectorResize}
        />
        <ResizablePanel
          id="session-trace-inspector"
          panelRef={inspectorPanelRef}
          collapsedSize={0}
          collapsible
          disabled={!splitInspectorOpen}
          minSize={compact ? 0 : sessionTraceInspectorMinWidth}
          maxSize={maximumInspectorWidth}
          defaultSize={sessionTraceInspectorDefaultWidth}
          groupResizeBehavior="preserve-pixel-size"
          className="min-w-0"
          style={sessionTraceInspectorPanelStyle(compact, inspectorOpen)}
          onResize={(size) => {
            if (!splitInspectorOpen || size.inPixels <= 0 || !userResizingInspectorRef.current) {
              return;
            }
            const roundedWidth = Math.round(size.inPixels);
            setInspectorWidth(roundedWidth);
          }}
        >
          <div
            aria-hidden={!inspectorOpen}
            data-session-trace-inspector-viewport=""
            className={inspectorViewportClassName}
          >
            {inspector}
          </div>
        </ResizablePanel>
      </ResizablePanelGroup>
    </div>
  );
}

function sessionTraceInspectorPanelStyle(compact: boolean, inspectorOpen: boolean) {
  return { overflow: inspectorOpen && compact ? 'visible' : 'hidden' } as const;
}

function sessionTraceInspectorViewportClassName(compact: boolean, inspectorOpen: boolean) {
  return clsx(
    'h-full min-h-0 min-w-0 overflow-hidden',
    !inspectorOpen && 'hidden',
    compact && inspectorOpen && 'absolute inset-0 z-20 bg-card',
  );
}
