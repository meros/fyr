// SPEC V0.132: where a new project or thread lives, with several hosts. One field for the desktop and the phone (New
// thread, New project); the phone's 44 px comes from its stylesheet. The hosts come from the shell (HostsContext), so a
// form deep in a screen needs no props for them.
import { HostMark } from "./host-mark-ui.tsx";
import { createContext } from "preact";
import { useContext } from "preact/hooks";
import { hostChoices, hostOfflineNote, type HostInfo } from "./hosts.ts";
import type { Profile } from "./types.ts";

/** V0.170: machine is what this install runs on, for its own mark when hosts[] does not name it (one host). */
export const HostsContext = createContext<{ hosts: HostInfo[]; self: string; now: number; machine?: string }>({ hosts: [], self: "", now: 0 });
export const useHosts = () => useContext(HostsContext);

/** The providers of every host, merged: the handover divider's names (V0.45), the inbox rows' provider (V0.206). */
export const ProfilesContext = createContext<Profile[]>([]);

/** The Host field, where the host is a choice (New project, New thread: host first, V0.139): nothing with one host;
 *  an offline host's note says why nothing can be made there. */
export function HostField({ value, onPick, what = "It lives there: its folders and providers are that host's." }: { value: string; onPick(h: string): void; what?: string }) {
  const { hosts, self, now } = useHosts();
  const choices = hostChoices(hosts, self);
  if (!choices.length) return null;
  const note = hostOfflineNote(value, hosts, now);
  return (
    <label class="fld hostf">
      <span>Host</span>
      {/* V0.170: the picked host's mark beside the list (a native list draws no icons in its options). */}
      <span class="hostsel">
        <HostMark name={value} size={20} />
        <select value={value} onChange={(e) => onPick((e.target as HTMLSelectElement).value)}>
          {choices.map((c) => (
            <option key={c.name} value={c.name}>
              {c.label}
            </option>
          ))}
        </select>
      </span>
      {note ? (
        <span class="srcl hbad" role="alert">
          {note}
        </span>
      ) : (
        <span class="srcl">{what}</span>
      )}
    </label>
  );
}

/** V0.139: the host of an existing project or thread, read-only, in the same place on every screen: its name in mono,
 *  and "offline since 19:40 · read-only" when it is. */
export function HostChip({ chip }: { chip: { name: string; line: string; offline: boolean } }) {
  return (
    <span class={`hchip${chip.offline ? " off" : ""}`} title={`Runs on ${chip.name}`}>
      <span class="hdot" aria-hidden="true" />
      <span class="mono">{chip.name}</span>
      {chip.line && <span class="hchip-l"> · {chip.line}</span>}
    </span>
  );
}
