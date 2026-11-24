// V0.settings-ia: the two ways into Settings, the desktop rail's gear and the phone's More row, show the one count
// (settings-nav.ts settingsCounts) and name the pages that hold what is counted, so a login that expired is seen
// before a thread fails on it, and the number at the door equals the nav's badges inside.

import { useApi } from "./api-context.ts";
import { useContext } from "preact/hooks";
import { ProfilesContext, useHosts } from "./host-pick-ui.tsx";
import { LINE_ICONS } from "./lucide.ts";
import { profilesKey, useSettingsCounts } from "./settings-count.ts";
import { SETTINGS_HOLDS, needLine, needWords } from "./settings-nav.ts";

/** What an entry point shows: the count, its words ("2 need you"), and the line that names the pages ("Sign-in 1 ·
 *  Providers 1"), or what Settings holds while nothing waits. Both entry points count every host Settings lists, the
 *  ones this device hides too (the shell gives them as `all`), so the number is the one inside. */
export function useSettingsEntry(): { count: number; need: string; line: string } {
  const hs = useHosts();
  const c = useSettingsCounts(useApi(), hs.all ?? hs.hosts, profilesKey(useContext(ProfilesContext)), !!hs.offline);
  const count = c?.total ?? 0;
  return { count, need: needWords(count), line: (c && needLine(c)) || SETTINGS_HOLDS };
}

/** The rail's gear (desktop). The phone has no rail: its entry is the More sheet's Settings row (phone-sheets.tsx),
 *  which shows the same count and line. */
export function SettingsGear({ onClick }: { onClick(): void }) {
  const e = useSettingsEntry();
  return (
    <button class="ri rgear" title={e.count ? `Settings · ${e.need}: ${e.line}` : `Settings: ${e.line}`} aria-label={e.count ? `Settings, ${e.need}: ${e.line}` : "Settings"} onClick={onClick}>
      <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" dangerouslySetInnerHTML={{ __html: LINE_ICONS.settings }} />
      <span class="l">Settings</span>
      {e.count > 0 && <span class="rb">{e.count}</span>}
    </button>
  );
}
