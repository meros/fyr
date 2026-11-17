// SPEC V0.164: what the undeclared-work components share: the warning mark and the app's way to go somewhere.

import { createContext } from "preact";
import { useContext } from "preact/hooks";
import type { Lookup } from "./load.ts";
import type { PageRef } from "./settings-nav.ts";

/** What the components cannot know alone: a thread's title and project, and the app's two ways to go somewhere (the
 *  desktop closes Settings and selects the row, the phone pushes a screen). */
export interface UndNav {
  lookup: Lookup;
  openThread(id: string): void;
  openSettings(at: PageRef): void;
}
export const UndContext = createContext<UndNav>({ lookup: (id) => ({ title: id }), openThread: () => {}, openSettings: () => {} });
export const useUnd = () => useContext(UndContext);

/** The warning mark (a triangle). */
export function UndIcon({ size = 12 }: { size?: number }) {
  return (
    <svg class="und-ic" width={size} height={size} viewBox="0 0 16 16" aria-hidden="true">
      <path d="M8 2.2 14.3 13.2H1.7Z" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
      <path d="M8 6.6v3.2" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
      <circle cx="8" cy="11.4" r=".9" fill="currentColor" />
    </svg>
  );
}
