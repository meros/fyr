// V0.model-choice (#18, #23): a provider and its model as one readable choice ("Claude · Opus 5.5"), effort only for
// a model that takes it, and the nearest model and effort on another provider (Hand over). Pure; the New thread
// form (desktop and phone) and Hand over (one form on both fronts) read it.
import type { Profile } from "./types.ts";
import { shortModel } from "./format.ts";
import { effortsFor } from "./providers.ts";

/** The effort levels from least to most, for "the nearest one". */
const EFFORT_ORDER = ["low", "medium", "high", "xhigh", "max"];

/** A model's readable name: the provider's own name for it, else the id made readable ("claude-opus-5-5" → "Opus 5.5"). */
export function modelName(p: Profile | undefined, id: string): string {
  const named = p?.modelCaps?.find((m) => m.id === id)?.name;
  if (named) return named;
  const s = shortModel(id);
  return /^claude-/.test(id) ? s.charAt(0).toUpperCase() + s.slice(1) : s;
}

/** "Claude · Opus 5.5"; the model "" is the CLI's own default. */
export function choiceLabel(p: Profile, model: string): string {
  return `${p.name} · ${model ? modelName(p, model) : "its default model"}`;
}

interface ModelChoice {
  /** the select's value: profile and model, joined by a NUL */
  value: string;
  profile: string;
  model: string;
  label: string;
  /** why it cannot be picked (V0.50: a provider that cannot run is shown, not pickable) */
  why?: string;
}

const SEP = "\u0000";
export const choiceValue = (profile: string, model: string) => `${profile}${SEP}${model}`;
export function parseChoice(v: string): { profile: string; model: string } {
  const i = v.indexOf(SEP);
  return i < 0 ? { profile: v, model: "" } : { profile: v.slice(0, i), model: v.slice(i + 1) };
}

/** Every provider's models as one list, in the owner's order: a provider with no list is one choice (its CLI's default),
 *  one that cannot run is one choice that cannot be picked, and the picked model stays even when it is turned off. */
export function modelChoices(profiles: Profile[], picked?: { profile: string; model: string }): ModelChoice[] {
  const out: ModelChoice[] = [];
  for (const p of profiles) {
    if (p.unavailable) {
      out.push({ value: choiceValue(p.id, ""), profile: p.id, model: "", label: `${p.name} · needs setup`, why: p.unavailable });
      continue;
    }
    const ms = p.models.length ? p.models : [""];
    for (const m of ms) out.push({ value: choiceValue(p.id, m), profile: p.id, model: m, label: choiceLabel(p, m) });
    if (picked?.profile === p.id && picked.model && !ms.includes(picked.model)) {
      out.push({ value: choiceValue(p.id, picked.model), profile: p.id, model: picked.model, label: `${choiceLabel(p, picked.model)} (off)` });
    }
  }
  return out;
}

/** "opus" of "claude-opus-5-5", "flash" of "gemini-3.8-flash", "neon" of "gemini-4-neon"; "" for an id that names no family. */
function family(id: string): string {
  const r = /^claude-([a-z]+)-\d/.exec(id);
  if (r) return r[1];
  const g = /^gemini-(?:[\d.]+-)?([a-z]+)/.exec(id);
  return g ? g[1] : "";
}

/** The model a thread on `from` gets on `to`: the same id when `to` offers it, else the first of the same family in the
 *  owner's order, else `to`'s default (its first, else the CLI's own ""). */
export function nearestModel(from: string, to: Profile | undefined): string {
  if (!to) return "";
  if (from && to.models.includes(from)) return from;
  const fam = family(from);
  const same = fam ? to.models.find((m) => family(m) === fam) : undefined;
  return same ?? to.defaultModel ?? to.models[0] ?? "";
}

/** The effort on a model that takes `efforts`: the asked one, else the nearest by EFFORT_ORDER (the lower on a tie),
 *  else the provider's default; "" when the model takes none or nothing was asked and there is no default. */
export function nearestEffort(asked: string, efforts: string[], fallback = ""): string {
  if (!efforts.length) return "";
  if (asked && efforts.includes(asked)) return asked;
  const ai = EFFORT_ORDER.indexOf(asked);
  if (ai >= 0) {
    let best = "";
    let bd = Infinity;
    for (const e of efforts) {
      const d = Math.abs(EFFORT_ORDER.indexOf(e) - ai);
      if (EFFORT_ORDER.includes(e) && d < bd) [best, bd] = [e, d];
    }
    if (best) return best;
  }
  return fallback && efforts.includes(fallback) ? fallback : "";
}

/** What Hand over starts with on `to`: the nearest model and effort to the thread's. */
export function handoverPick(t: { model?: string; effort?: string }, from: Profile | undefined, to: Profile | undefined): { model: string; effort: string } {
  const model = nearestModel(t.model ?? "", to);
  const asked = t.effort || (from && t.model ? nearestEffort(from.defaultEffort ?? "", effortsFor(from, t.model)) : "");
  return { model, effort: nearestEffort(asked, effortsFor(to, model), to?.defaultEffort) };
}

const withEffort = (name: string, effort: string) => (effort ? `${name} · ${effort}` : name);

/** The line under Hand over's choice: what the thread had and what it gets, and whether that is the nearest match. */
export function mappingLine(t: { model?: string; effort?: string }, from: Profile | undefined, to: Profile | undefined, pick: { model: string; effort: string }): string {
  if (!to) return "";
  const was = withEffort(t.model ? modelName(from, t.model) : "the default model", t.effort ?? "");
  const now = withEffort(pick.model ? modelName(to, pick.model) : "its default model", pick.effort);
  if (was === now) return `Keeps ${now}.`;
  // V0.dialogs-review: the same model is not a change: only the effort is named, and only when the thread had one
  // (a thread without an asked effort gets the model's default, which is no arrow from nothing).
  const model = pick.model ? modelName(to, pick.model) : "";
  if (model && t.model && modelName(from, t.model) === model) return t.effort ? `Keeps ${model}; effort ${t.effort} → ${pick.effort || "the default"}.` : `Keeps ${model} · effort ${pick.effort}.`;
  const near = handoverPick(t, from, to);
  const nearest = near.model === pick.model && near.effort === pick.effort;
  return nearest ? `${was} → ${now}: the nearest ${to.name} has.` : `${was} → ${now}.`;
}
