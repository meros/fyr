// SPEC V0.132, V0.host-tabs: where a new project, thread or handover lives, with several hosts. One tabbed host
// bar (HostTabs) for the desktop and the phone; the phone's 44 px comes from its stylesheet. The hosts come from the
// shell (HostsContext), so a form deep in a screen needs no props for them.
import { createContext } from "preact";
import { useContext } from "preact/hooks";
import { hostChoices, hostOfflineNote, type HostInfo, type HostProfiles } from "./hosts.ts";
import { asHostId, type HostId } from "./hostid.ts";
import type { HostTabItem } from "./host-tabs.ts";
import { HostTabs } from "./host-tabs-ui.tsx";

/** The front's hosts, its own among them (decision 0031 2.6: nothing here says which one is the front's). */
export const HostsContext = createContext<{ hosts: HostInfo[]; now: number }>({ hosts: [], now: 0 });
export const useHosts = () => useContext(HostsContext);

/** Each host's providers by install id (slice 2b: no merged list): a thread's provider is its own host's (V0.206). */
export const ProfilesContext = createContext<HostProfiles>({});

/** The Host field, where the host is a choice (New project, New thread, Hand over: host first, V0.139; tabbed
 *  with each host's mark in V0.host-tabs): nothing with one host; an offline host's note says why nothing can be made there. */
export function HostField({ value, onPick, what = "It lives there: its folders and providers are that host's.", quiet, here }: { /** V0.dialogs-review: the host the thing is on now (Hand over's thread): its tab says "(now)" */ here?: HostId; value: HostId | undefined; onPick(h: HostId): void; what?: string; /** V0.dialogs-review: no standing help line (New thread, where the field is one of several under the prompt): `what` is the label's tooltip, and the offline note still shows */ quiet?: boolean }) {
  const { hosts, now } = useHosts();
  const choices = hostChoices(hosts);
  if (!choices.length) return null;
  const note = hostOfflineNote(value, hosts, now);
  const items: HostTabItem[] = choices.map((c) => ({
    id: c.id,
    name: c.name,
    label: here && c.id === here ? `${c.name} (now)` : c.name,
    offline: !c.online,
    title: c.label,
  }));
  return (
    <div class="fld hostf">
      <span title={quiet ? what : undefined}>Host</span>
      <HostTabs
        items={items}
        active={value ?? ""}
        label="Host"
        compact
        onPick={(id) => {
          const h = asHostId(id);
          if (h) onPick(h);
        }}
      />
      {note ? (
        <span class="srcl hbad" role="alert">
          {note}
        </span>
      ) : (
        !quiet && <span class="srcl">{what}</span>
      )}
    </div>
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
