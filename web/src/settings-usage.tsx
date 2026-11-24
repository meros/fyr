/** V0.usage-page (decision 0041): Settings → Usage, one page on the desktop and the phone. The tokens every host
 *  counted, over a range, by one key at a time, with Fyr's own calls apart. Tokens only: no money. A tap on a row
 *  narrows the page to it (a model, a project, an account…); a thread's row opens the thread. */
import { useEffect, useRef, useState } from "preact/hooks";
import type { Api } from "./api.ts";
import { Empty, Group, LoadError, Row, Seg, Skeleton } from "./settings-rows.tsx";
import { UsageBars } from "./usage-ui.tsx";
import {
  BYS,
  RANGES,
  filterOf,
  globalWhere,
  kindsLine,
  mergeUsage,
  overheadRows,
  ownCallsFoot,
  perDay,
  perHostWhere,
  rangeDays,
  tokens,
  mergeOverhead,
  overheadKinds,
  type HostOverhead,
  type HostUsage,
  type UsageBy,
  type UsageFilter,
  type UsageRange,
  type UsageRow,
} from "./usage.ts";

interface Loaded {
  by: HostUsage[];
  days: HostUsage[];
  sources: HostUsage[];
  overhead: HostOverhead[];
  failed: string[];
}

type UsageApi = Partial<Pick<Api, "usage" | "usageOverhead">>;

function useUsage(api: UsageApi, range: UsageRange, by: UsageBy, filter: UsageFilter | null, now: number) {
  const [st, setSt] = useState<{ data: Loaded | null; err: string | null; key: string }>({ data: null, err: null, key: "" });
  const [tick, setTick] = useState(0);
  const days = rangeDays(range, now);
  const from = days[0];
  const to = days[days.length - 1];
  const key = `${range}|${by}|${from}|${tick}|${filter ? `${filter.key}=${JSON.stringify(filter.hosts)}` : ""}`;
  // The shell may hand a new api object on each render: the read follows the query, never the object's identity,
  // or every answer would start the next read.
  const apiRef = useRef(api);
  apiRef.current = api;
  const filterRef = useRef(filter);
  filterRef.current = filter;
  useEffect(() => {
    const a = apiRef.current;
    if (!a.usage) return;
    const read = a.usage.bind(a);
    const f = filterRef.current;
    const where = globalWhere(f);
    const only = perHostWhere(f);
    let live = true;
    const group = by === "host" ? [] : [by];
    const none = Promise.resolve({ hosts: [] as HostOverhead[], failed: [] as string[] });
    // The overhead is of the turns: a filter on a model or a part (not a turn's) shows none.
    const ov = a.usageOverhead && !(f && (f.key === "model" || f.key === "part")) ? a.usageOverhead.bind(a)({ from, to, group: [], where }, only) : none;
    Promise.all([read({ from, to, group, where }, only), read({ from, to, group: ["day"], where }, only), read({ from, to, group: ["source"], where }, only), ov])
      .then(([x, d, s, o]) => live && setSt({ data: { by: x.hosts, days: d.hosts, sources: s.hosts, overhead: o.hosts, failed: [...new Set([...x.failed, ...d.failed, ...s.failed, ...o.failed])] }, err: null, key }))
      .catch((e: unknown) => live && setSt({ data: null, err: e instanceof Error ? e.message : "The hosts did not answer", key }));
    return () => {
      live = false;
    };
  }, [key]); // eslint-disable-line react-hooks/exhaustive-deps -- key names the range, the grouping, the filter and the retry
  return { ...st, loading: st.key !== key, days, retry: () => setTick((t) => t + 1) };
}

/** A row: the name, its share of the biggest row as a thin bar, the total; each kind in its line. A tap narrows the
 *  page to it, or opens a thread. */
function UsageItem({ r, max, onPick, action }: { r: UsageRow; max: number; onPick?(): void; action: string }) {
  return (
    <Row
      class="ug-r"
      title={r.label}
      ell
      onClick={onPick}
      data={{ row: r.id }}
      desc={
        <>
          {r.sub && <span class="ug-host">{r.sub} · </span>}
          {kindsLine(r)}
          <span class="ug-bar" aria-hidden="true">
            <i style={{ width: `${Math.max(1, Math.round((r.total / max) * 100))}%` }} />
          </span>
        </>
      }
      end={
        <span class="ug-n mono" title={`${r.total.toLocaleString()} tokens${onPick ? ` · ${action}` : ""}`}>
          {tokens(r.total)}
        </span>
      }
    />
  );
}

function Rows({ rows, empty, pick, action }: { rows: UsageRow[]; empty: string; pick?(r: UsageRow): (() => void) | undefined; action: string }) {
  if (!rows.length) return <Empty text={empty} />;
  const max = Math.max(1, ...rows.map((r) => r.total));
  return (
    <>
      {rows.slice(0, 50).map((r) => (
        <UsageItem key={r.id} r={r} max={max} onPick={pick?.(r)} action={action} />
      ))}
      {rows.length > 50 && <Row class="ug-more" desc={`${rows.length - 50} more, each smaller than these`} />}
    </>
  );
}

export function UsagePage({ api, now, onOpenThread }: { api: UsageApi; now: number; onOpenThread?(id: string): void }) {
  const [range, setRange] = useState<UsageRange>("7d");
  const [by, setBy] = useState<UsageBy>("model");
  const [filter, setFilter] = useState<UsageFilter | null>(null);
  const u = useUsage(api, range, by, filter, now);
  if (!api.usage) return <Empty text="This front cannot read usage." />;
  const byLabel = (k: UsageBy) => BYS.find((b) => b.value === k)?.label ?? k;
  const controls = (
    <Group>
      <Row title="Range" col end={<Seg<UsageRange> value={range} label="Range" options={RANGES} onChange={setRange} />} />
      <Row
        title="By"
        end={
          <select class="ug-by" aria-label="Group by" value={by} onChange={(e) => setBy((e.target as HTMLSelectElement).value as UsageBy)}>
            {BYS.filter((b) => b.value !== filter?.key).map((b) => (
              <option value={b.value}>{b.label}</option>
            ))}
          </select>
        }
      />
      {filter && (
        <Row
          title="Only"
          desc={`${byLabel(filter.key)}: ${filter.label}`}
          end={
            <button type="button" class="btn" onClick={() => setFilter(null)}>
              Show all
            </button>
          }
        />
      )}
    </Group>
  );
  if (u.err) return <>{controls}<LoadError text={u.err} onRetry={u.retry} /></>;
  if (!u.data) return <>{controls}<Skeleton rows={6} label="Reading usage" /></>;
  const d = u.data;
  const dayRows = mergeUsage(d.days, "day");
  const sum = (k: keyof Pick<UsageRow, "input" | "output" | "cacheRead" | "cacheWrite5m" | "cacheWrite1h" | "thinking" | "total">) => dayRows.reduce((n, r) => n + r[k], 0);
  const total = { input: sum("input"), output: sum("output"), cacheRead: sum("cacheRead"), cacheWrite5m: sum("cacheWrite5m"), cacheWrite1h: sum("cacheWrite1h"), thinking: sum("thinking"), total: sum("total") };
  const fyr = overheadRows(d.sources);
  const fyrTotal = fyr.reduce((n, r) => n + r.total, 0);
  const days = perDay(d.days, u.days);
  const pick = (k: UsageBy) => (r: UsageRow) => {
    if (k === "thread") return onOpenThread ? () => onOpenThread(r.id) : undefined;
    const f = filterOf(k, r);
    return f ? () => setFilter(f) : undefined;
  };
  return (
    <div class={`ug${u.loading ? " busy" : ""}`}>
      {controls}
      <Group title="Tokens" foot={`A plain sum of the kinds, a count and not a weight: a cache read costs far less of a quota than an output token. Subagents and the CLI's own calls are in it.${d.failed.length ? ` Not read: ${d.failed.join(", ")}.` : ""}`}>
        <Row title={<span class="ug-big mono">{tokens(total.total)}</span>} desc={kindsLine(total)} />
        {range !== "today" && <UsageBars bars={days.map((x) => ({ at: x.day, total: x.total }))} label={`Tokens per day, ${days[0]?.day} to ${days[days.length - 1]?.day}`} />}
      </Group>
      <Group title={`By ${byLabel(by).toLowerCase()}`}>
        <Rows rows={mergeUsage(d.by, by)} empty="No tokens counted in this range." pick={pick(by)} action={by === "thread" ? "open the thread" : "show only this"} />
      </Group>
      <OverheadGroup hosts={d.overhead} total={total.total} />
      <Group title="Fyr's own calls" foot={ownCallsFoot(fyrTotal, total.total)}>
        <Rows rows={fyr} empty="Fyr made no calls of its own in this range." pick={filter?.key === "source" ? undefined : pick("source")} action="show only this" />
      </Group>
    </div>
  );
}

/** V0.usage-overhead: Fyr's overhead inside the agents' turns, by part, with each part's share of the range. These
 *  tokens are inside the turns' totals above, not added to them. */
function OverheadGroup({ hosts, total }: { hosts: HostOverhead[]; total: number }) {
  const { lines, uncalibrated } = mergeOverhead(hosts);
  const sum = lines.reduce((n, l) => n + l.total, 0);
  if (!hosts.length) return null;
  const pct = (n: number) => (total ? `${Math.round((n / total) * 1000) / 10} %` : "0 %");
  return (
    <Group
      title="Fyr's overhead in agent turns"
      foot={`Inside the turns' tokens above, not on top of them: ${pct(sum)} of the range.${uncalibrated ? ` ${uncalibrated.toLocaleString()} requests wait for their fixed context to be measured.` : ""}`}
    >
      {lines.map((l) => (
        <Row class="ug-r ug-ov" title={l.label} desc={`${l.desc}. ${overheadKinds(l)}`} end={<span class="ug-n mono" title={`${l.total.toLocaleString()} tokens, ${pct(l.total)}`}>{tokens(l.total)}</span>} />
      ))}
    </Group>
  );
}
