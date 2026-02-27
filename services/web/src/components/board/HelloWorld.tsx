// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (c) 2026 Jared Redh. All rights reserved.

/**
 * HelloWorld — the first BIG BOARD component.
 *
 * Kind: static (hardcoded content, no network calls).
 */

export default function HelloWorld() {
  return (
    <div class="hw-component">
      <p class="hw-component__message">Hello, World.</p>
      <p class="hw-component__sub">Static content · no network calls</p>
    </div>
  );
}
