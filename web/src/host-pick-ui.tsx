// SPEC V0.132: where a new project or thread lives, with several hosts. One field for the desktop and the phone (New
// thread, New project); the phone's 44 px comes from its stylesheet. The hosts come from the shell (HostsContext), so a
// form deep in a screen needs no props for them.
import { createContext } from "preact";
import { useContext } from "preact/hooks";
import { hostChoices, hostOfflineNote, type HostInfo } from "./hosts.ts";

export const HostsContext = createContext<{ hosts: HostInfo[]; self: string; now: number }>({ hosts: [], self: "", now: 0 });
export const useHosts = () => useContext(HostsContext);

/** The Host field: nothing with one host. lock: the project that holds the host (the field shows it and cannot
 *  change); an offline host's note says why nothing can be made there. */
export function HostField({ value, onPick, lock, what = "It lives there: its folders and providers are that host's." }: { value: string; onPick(h: string): void; lock?: string; what?: string }) {
  const { hosts, self, now } = useHosts();
  const choices = hostChoices(hosts, self);
  if (!choices.length) return null;
  const note = hostOfflineNote(value, hosts, now);
  return (
    <label class="fld hostf">
      <span>Host</span>
      <select value={value} disabled={!!lock} onChange={(e) => onPick((e.target as HTMLSelectElement).value)}>
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
        <span class="srcl">{lock ? `Follows ${lock}.` : what}</span>
      )}
    </label>
  );
}
