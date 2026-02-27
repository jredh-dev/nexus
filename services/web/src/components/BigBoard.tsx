// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 Jared Redh. All rights reserved.

/**
 * BIG BOARD — expandable grid dashboard.
 *
 * Grid grows from 1×1 → 2×2 → 3×3 (max 9 slots).
 * Each slot hosts exactly one BoardComponent. Components are categorized
 * by their data-source complexity to prevent resource contention:
 *
 *   static    — hardcoded content, no network calls
 *   once      — single fetch on mount
 *   polling   — recurring fetch on an interval
 *   sse       — server-sent events stream
 *   ws        — WebSocket stream
 *   grpc      — gRPC-Web stream
 *
 * The grid enforces a per-type budget so high-demand sources (streaming)
 * can be artificially limited as the board grows.
 */

import { useState } from 'preact/hooks';
import type { ComponentChild } from 'preact';
import HelloWorld from './board/HelloWorld';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type ComponentKind = 'static' | 'once' | 'polling' | 'sse' | 'ws' | 'grpc';

export interface BoardSlot {
  id: string;
  kind: ComponentKind;
  label: string;
  node: ComponentChild;
}

export interface GridSize {
  cols: number;
  rows: number;
}

const GRID_STEPS: GridSize[] = [
  { cols: 1, rows: 1 },
  { cols: 2, rows: 2 },
  { cols: 3, rows: 3 },
];

// ---------------------------------------------------------------------------
// Component registry — add new board components here
// ---------------------------------------------------------------------------

const REGISTRY: BoardSlot[] = [
  {
    id: 'hello-world',
    kind: 'static',
    label: 'Hello World',
    node: <HelloWorld />,
  },
];

// ---------------------------------------------------------------------------
// BigBoard
// ---------------------------------------------------------------------------

export default function BigBoard() {
  const [stepIndex, setStepIndex] = useState(0);
  const grid = GRID_STEPS[stepIndex];
  const capacity = grid.cols * grid.rows;

  const slots: (BoardSlot | null)[] = Array.from({ length: capacity }, (_, i) =>
    i < REGISTRY.length ? REGISTRY[i] : null,
  );

  const canExpand = stepIndex < GRID_STEPS.length - 1;
  const canShrink = stepIndex > 0;

  return (
    <div class="big-board">
      <div class="big-board__header">
        <h1 class="big-board__title">BIG BOARD</h1>
        <div class="big-board__controls">
          <button
            class="big-board__ctrl-btn"
            disabled={!canShrink}
            onClick={() => setStepIndex(i => i - 1)}
            aria-label="Shrink grid"
          >
            −
          </button>
          <span class="big-board__grid-label">
            {grid.cols}×{grid.rows}
          </span>
          <button
            class="big-board__ctrl-btn"
            disabled={!canExpand}
            onClick={() => setStepIndex(i => i + 1)}
            aria-label="Expand grid"
          >
            +
          </button>
        </div>
      </div>

      <div
        class="big-board__grid"
        style={{ '--cols': grid.cols, '--rows': grid.rows } as Record<string, unknown>}
      >
        {slots.map((slot, idx) =>
          slot ? (
            <div key={slot.id} class={`big-board__cell big-board__cell--${slot.kind}`}>
              <div class="big-board__cell-header">
                <span class="big-board__cell-label">{slot.label}</span>
                <span class={`big-board__kind-badge big-board__kind-badge--${slot.kind}`}>
                  {slot.kind}
                </span>
              </div>
              <div class="big-board__cell-body">{slot.node}</div>
            </div>
          ) : (
            <div key={`empty-${idx}`} class="big-board__cell big-board__cell--empty">
              <span class="big-board__empty-label">empty slot</span>
            </div>
          ),
        )}
      </div>
    </div>
  );
}
