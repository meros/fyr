// SPEC V0.132: where a new project or thread lives, with several hosts. One field for the desktop and the phone (New
// thread, New project); the phone's 44 px comes from its stylesheet. The hosts come from the shell (HostsContext), so a
// form deep in a screen needs no props for them.
import { createContext } from "preact";
import { useContext } from "preact/hooks";
import { hostChoices, hostOfflineNote, type HostInfo } from "./hosts.ts";

export const HostsContext = createContext<{ hosts: HostInfo[]; self: string; now: number }>({ hosts: [], self: "", now: 0 });
export const useHosts = () => useContext(HostsContext);

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
      <select value={value} onChange={(e) => onPick((e.target as HTMLSelectElement).value)}>
        {choices.map((c) => (
          <option key={c.name} value={c.name}>
            {c.label}
          </option>
        ))}
      </select>
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
