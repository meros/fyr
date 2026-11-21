// V0.readable-approvals: what a tool approval card says to a person.
// Mirrors effects.ts (V0.27): the card leads with plain words and readable fields;
// the raw tool name and full JSON input stay behind Details.

import { formatApproval } from "./approval.ts";
import { friendlyName, middleTruncate, relPath } from "./conversation.ts";
import type { Decision } from "./types.ts";

export interface ApprovalField {
  label: string;
  value: string;
  /** render in monospace (paths, URLs, commands, IDs, code) */
  mono?: boolean;
}

export interface ApprovalText {
  /** short action word for the inbox row ("Run command", "Offer action", "Edit file") */
  label: string;
  /** one-line subject for inbox rows and peeks ("git push", "Write ADR and spec") */
  subject: string;
  /** card heading ("Run a command?", "Offer action “Write ADR and spec”?") */
  title: string;
  /** optional plain explanation or agent description */
  summary: string;
  /** optional verbatim shell command (always shown in monospace) */
  command: string;
  /** structured human-readable fields */
  fields: ApprovalField[];
  /** count of omitted fields when a generic tool has many inputs */
  hiddenFields: number;
  /** shortened working folder, when relevant to show inline */
  where: string;
  /** whether the folder/host line should show inline (otherwise kept in Details) */
  showWhere: boolean;
  /** V0.tool-approval-judge: the call runs with its sandbox bypassed, said as a badge */
  outsideSandbox: boolean;
  /** V0.tool-approval-judge: the judge's note, when a check could not say yes or no */
  judgeNote: string;
  /** raw values for the collapsible Details disclosure */
  details: { tool: string; input: Record<string, unknown>; cwd: string };
}

const MAX_GENERIC_FIELDS = 6;
const MONO_KEY = /(?:^|_)(?:path|file|dir|cwd|url|uri|cmd|command|branch|ref|sha|commit|head|base|id|pid|port|host|pattern|glob|regex|argv|code|diff|patch)$/i;

function str(v: unknown): string {
  return typeof v === "string" ? v.trim() : "";
}

function firstLine(s: string, max = 96): string {
  const line = (s.split("\n").find((x) => x.trim()) ?? "").replace(/\s+/g, " ").trim();
  return line.length > max ? middleTruncate(line, max) : line;
}

function clip(s: string, max = 56): string {
  const flat = s.replace(/\s+/g, " ").trim();
  return flat.length > max ? flat.slice(0, max - 1).trimEnd() + "…" : flat;
}

function shortHome(p: string): string {
  return p.replace(/^\/home\/[^/]+(?=\/|$)/, "~").replace(/^\/Users\/[^/]+(?=\/|$)/, "~");
}

function urlHost(url: string): string {
  const m = /^[a-z]+:\/\/([^/]+)/i.exec(url);
  return m ? m[1] : "";
}

/** Convert snake_case or camelCase keys to readable labels: "file_path" → "File path". */
export function humanKey(key: string): string {
  const words = key
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[_-]+/g, " ")
    .trim()
    .toLowerCase();
  return words ? words[0].toUpperCase() + words.slice(1) : key;
}

function formatValue(key: string, v: unknown, cwd: string): ApprovalField | null {
  if (v === null || v === undefined || v === "") return null;
  if (typeof v === "string") {
    const trimmed = v.trim();
    if (!trimmed) return null;
    const isPath = /(?:^|_)(?:file_path|notebook_path|path|dir|cwd)$/i.test(key) && (trimmed.startsWith("/") || trimmed.startsWith("~"));
    const value = isPath ? (cwd ? relPath(trimmed, cwd, 80) : shortHome(trimmed)) : trimmed;
    const mono = MONO_KEY.test(key) || /^https?:\/\//i.test(trimmed);
    return { label: humanKey(key), value, ...(mono ? { mono: true } : {}) };
  }
  if (typeof v === "boolean") return { label: humanKey(key), value: v ? "Yes" : "No" };
  if (typeof v === "number") return { label: humanKey(key), value: String(v) };
  if (Array.isArray(v)) {
    if (v.length === 0) return null;
    if (v.every((x) => typeof x === "string" || typeof x === "number")) {
      return { label: humanKey(key), value: v.map(String).join(", "), ...(MONO_KEY.test(key) ? { mono: true } : {}) };
    }
    return { label: humanKey(key), value: JSON.stringify(v, null, 2), mono: true };
  }
  if (typeof v === "object") {
    const entries = Object.entries(v as Record<string, unknown>);
    if (entries.length === 0) return null;
    return { label: humanKey(key), value: JSON.stringify(v, null, 2), mono: true };
  }
  return { label: humanKey(key), value: String(v) };
}

function genericFields(input: Record<string, unknown>, cwd: string, skip: Set<string>): { fields: ApprovalField[]; hidden: number } {
  const all: ApprovalField[] = [];
  for (const [k, v] of Object.entries(input)) {
    if (skip.has(k)) continue;
    const f = formatValue(k, v, cwd);
    if (f) all.push(f);
  }
  return { fields: all.slice(0, MAX_GENERIC_FIELDS), hidden: Math.max(0, all.length - MAX_GENERIC_FIELDS) };
}

function lineCount(s: string): string {
  const n = s ? s.split("\n").length : 0;
  return n === 1 ? "1 line" : `${n} lines`;
}

function describeWatchWhen(when: unknown): string {
  if (!when || typeof when !== "object") return "";
  const w = when as Record<string, unknown>;
  switch (w.kind) {
    case "at":
      return str(w.at) ? `At ${str(w.at)}` : "At a scheduled time";
    case "changeReached":
      return `When ${str(w.change) || "the change"} reaches ${str(w.phase) || "the target phase"}`;
    case "condition":
      return `When ${str(w.type) || "condition"} on ${str(w.change) || "the change"} is ${str(w.status) || "ready"}`;
    case "quota":
      return `When the${str(w.window) ? " " + str(w.window) : ""} quota window resets`;
    case "workDone":
      return "When all work is done";
    case "runEnded":
      return "When the current run ends";
    case "idleFor":
      return typeof w.ms === "number" ? `After idle for ${Math.round(w.ms / 1000)} s` : "After idle";
    default:
      return JSON.stringify(when, null, 2);
  }
}

export function describeApproval(d: Pick<Decision, "tool" | "input"> & Pick<Partial<Decision>, "judgeNote">, threadCwd = ""): ApprovalText {
  const tool = (d.tool ?? "").trim();
  const input = (d.input && typeof d.input === "object" ? d.input : {}) as Record<string, unknown>;
  const rawCwd = str(input.cwd) || threadCwd;
  const where = rawCwd ? shortHome(rawCwd) : "";
  const details = { tool, input, cwd: rawCwd };
  // V0.tool-approval-judge: the sandbox bypass and the judge's note are the card's, not the tool formatter's.
  const fa = formatApproval({ input, judgeNote: d.judgeNote });
  const base = (p: Partial<ApprovalText> & Pick<ApprovalText, "label" | "title">): ApprovalText => ({
    subject: "",
    summary: "",
    command: "",
    fields: [],
    hiddenFields: 0,
    where,
    showWhere: false,
    outsideSandbox: fa.outsideSandbox,
    judgeNote: fa.judgeNote ?? "",
    details,
    ...p,
  });

  if (!tool) {
    const g = genericFields(input, rawCwd, new Set(["cwd"]));
    return base({ label: "Allow tool", title: "Allow this tool?", fields: g.fields, hiddenFields: g.hidden });
  }

  const path = str(input.file_path) || str(input.notebook_path) || str(input.path);
  const shortPath = path ? relPath(path, rawCwd, 56) : "";

  switch (tool) {
    case "Bash": {
      const command = fa.cmd ?? "";
      const desc = str(input.description);
      const fields: ApprovalField[] = [];
      if (input.run_in_background === true) fields.push({ label: "Mode", value: "Runs in the background" });
      if (typeof input.timeout === "number" && input.timeout > 0) fields.push({ label: "Timeout", value: `${Math.round(input.timeout / 1000)} s` });
      return base({
        label: "Run command",
        subject: firstLine(command || desc, 80),
        title: "Run a command?",
        summary: desc,
        command,
        fields,
        showWhere: true,
      });
    }

    case "Edit":
    case "MultiEdit":
    case "NotebookEdit": {
      const fields: ApprovalField[] = [];
      const oldStr = typeof input.old_string === "string" ? input.old_string : "";
      const newStr = typeof input.new_string === "string" ? input.new_string : "";
      if (Array.isArray(input.edits) && input.edits.length > 0) {
        fields.push({ label: "Edits", value: input.edits.length === 1 ? "1 replacement" : `${input.edits.length} replacements` });
      } else {
        if (oldStr) fields.push({ label: `Replace (${lineCount(oldStr)})`, value: oldStr, mono: true });
        if (newStr) fields.push({ label: `With (${lineCount(newStr)})`, value: newStr, mono: true });
      }
      if (input.replace_all === true) fields.push({ label: "Scope", value: "Replace all occurrences" });
      return base({
        label: "Edit file",
        subject: shortPath,
        title: shortPath ? `Edit ${shortPath}?` : "Edit a file?",
        fields,
      });
    }

    case "Write": {
      const content = typeof input.content === "string" ? input.content : "";
      const fields: ApprovalField[] = [];
      if (content) fields.push({ label: `Content (${lineCount(content)})`, value: content, mono: true });
      return base({
        label: "Write file",
        subject: shortPath,
        title: shortPath ? `Write ${shortPath}?` : "Write a file?",
        fields,
      });
    }

    case "Read":
    case "NotebookRead": {
      const fields: ApprovalField[] = [];
      const offset = typeof input.offset === "number" ? input.offset : null;
      const limit = typeof input.limit === "number" ? input.limit : null;
      if (offset !== null || limit !== null) {
        const range = offset !== null && limit !== null ? `${offset}–${offset + limit - 1}` : offset !== null ? `from line ${offset}` : `first ${limit} lines`;
        fields.push({ label: "Lines", value: range });
      }
      return base({
        label: "Read file",
        subject: shortPath,
        title: shortPath ? `Read ${shortPath}?` : "Read a file?",
        fields,
      });
    }

    case "Grep": {
      const pattern = str(input.pattern);
      const fields: ApprovalField[] = [];
      if (shortPath) fields.push({ label: "In", value: shortPath, mono: true });
      if (str(input.glob)) fields.push({ label: "Files", value: str(input.glob), mono: true });
      if (str(input.type)) fields.push({ label: "Type", value: str(input.type) });
      return base({
        label: "Search code",
        subject: pattern,
        title: pattern ? `Search for “${clip(pattern, 48)}”?` : "Search the codebase?",
        fields,
      });
    }

    case "Glob":
    case "LS": {
      const pattern = str(input.pattern);
      const fields: ApprovalField[] = [];
      if (pattern && shortPath) fields.push({ label: "In", value: shortPath, mono: true });
      return base({
        label: "List files",
        subject: pattern || shortPath,
        title: pattern ? `Find files matching ${clip(pattern, 48)}?` : shortPath ? `List ${shortPath}?` : "List files?",
        fields,
      });
    }

    case "WebSearch": {
      const query = str(input.query);
      return base({
        label: "Search web",
        subject: query,
        title: query ? `Search the web for “${clip(query, 48)}”?` : "Search the web?",
      });
    }

    case "WebFetch": {
      const url = str(input.url);
      const host = urlHost(url);
      const fields: ApprovalField[] = [];
      if (url) fields.push({ label: "URL", value: url, mono: true });
      if (str(input.prompt)) fields.push({ label: "Prompt", value: str(input.prompt) });
      return base({
        label: "Fetch page",
        subject: host || firstLine(url, 60),
        title: host ? `Fetch ${host}?` : "Fetch a web page?",
        fields,
      });
    }

    case "Task":
    case "Agent": {
      const desc = str(input.description);
      const prompt = str(input.prompt);
      const role = str(input.subagent_type);
      const fields: ApprovalField[] = [];
      if (role) fields.push({ label: "Agent", value: role });
      if (prompt) fields.push({ label: "Prompt", value: prompt });
      return base({
        label: "Start subagent",
        subject: desc || firstLine(prompt, 72),
        title: desc ? `Start subagent: ${clip(desc, 48)}?` : "Start a subagent?",
        fields,
      });
    }

    case "Skill": {
      const skill = str(input.skill);
      const fields: ApprovalField[] = [];
      if (str(input.args)) fields.push({ label: "Arguments", value: str(input.args) });
      return base({
        label: "Run skill",
        subject: skill,
        title: skill ? `Run skill “${clip(skill, 48)}”?` : "Run a skill?",
        fields,
      });
    }
  }

  if (tool.startsWith("mcp__fyr__")) {
    const name = tool.slice("mcp__fyr__".length);
    switch (name) {
      case "offer_action": {
        const on = str(input.on) || "thread";
        const id = str(input.id);
        if (input.remove === true) {
          return base({
            label: "Remove action",
            subject: id,
            title: id ? `Remove action “${clip(id, 48)}”?` : "Remove an action?",
            summary: `Removes the ${on === "view" ? "live view" : "thread"} action button.`,
          });
        }
        const btnLabel = str(input.label);
        const prompt = str(input.prompt);
        const target = str(input.in) === "new" ? "that starts a new thread" : "on this thread";
        const summary =
          on === "view"
            ? `Adds a button to the live view ${str(input.run) === "script" ? "that runs a script" : "that sends a prompt"}.`
            : `Adds a button under the agent’s message ${target}.`;
        const fields: ApprovalField[] = [];
        if (prompt) fields.push({ label: "Prompt", value: prompt });
        if (Array.isArray(input.argv) && input.argv.length > 0) {
          fields.push({ label: "Command", value: input.argv.map(String).join(" "), mono: true });
        }
        if (str(input.cwd)) fields.push({ label: "Folder", value: shortHome(str(input.cwd)), mono: true });
        return base({
          label: "Offer action",
          subject: btnLabel || id,
          title: btnLabel ? `Offer action “${clip(btnLabel, 48)}”?` : "Offer an action?",
          summary,
          fields,
        });
      }

      case "run_request": {
        const command = fa.cmd ?? "";
        const why = str(input.why);
        return base({
          label: "Request command",
          subject: why || firstLine(command, 72),
          title: input.root === true ? "Ask to run a command as root?" : "Ask to run a command?",
          summary: why,
          command,
          showWhere: true,
        });
      }

      case "ask": {
        const qs = Array.isArray(input.questions) ? (input.questions as Record<string, unknown>[]) : [];
        const first = qs[0] ? str(qs[0].question) : "";
        const fields: ApprovalField[] = qs.slice(0, 4).map((q, i) => ({
          label: str(q.header) || (qs.length > 1 ? `Question ${i + 1}` : "Question"),
          value: str(q.question),
        })).filter((f) => f.value);
        return base({
          label: "Ask owner",
          subject: first,
          title: qs.length > 1 ? `Ask you ${qs.length} questions?` : "Ask you a question?",
          fields,
        });
      }

      case "settle_request": {
        const why = str(input.why);
        return base({
          label: "Settle thread",
          subject: why,
          title: "Mark this thread done?",
          summary: why,
        });
      }

      case "watch": {
        const remove = str(input.remove);
        if (remove) {
          return base({
            label: "Remove watch",
            subject: remove,
            title: "Remove a watch?",
            fields: [{ label: "Watch", value: remove, mono: true }],
          });
        }
        const then = str(input.then);
        const note = str(input.note);
        const when = describeWatchWhen(input.when);
        const fields: ApprovalField[] = [];
        if (when) fields.push({ label: "When", value: when, ...(when.startsWith("{") ? { mono: true } : {}) });
        if (note) fields.push({ label: "Note", value: note });
        return base({
          label: then === "settle" ? "Watch to settle" : "Set watch",
          subject: note || firstLine(when, 72),
          title: then === "settle" ? "Settle when a condition is met?" : "Watch for an event?",
          fields,
        });
      }

      case "change_open": {
        const t = str(input.title);
        const fields: ApprovalField[] = [];
        if (str(input.branch)) fields.push({ label: "Branch", value: str(input.branch), mono: true });
        if (str(input.pr)) fields.push({ label: "Pull request", value: str(input.pr), mono: true });
        if (input.workspace === true) fields.push({ label: "Worktree", value: "Create isolated git worktree" });
        return base({
          label: "Open change",
          subject: t || str(input.branch) || str(input.pr),
          title: t ? `Open change “${clip(t, 48)}”?` : "Open a code change?",
          fields,
        });
      }

      case "change_update": {
        const ch = str(input.change);
        const fields: ApprovalField[] = [];
        if (str(input.phase)) fields.push({ label: "Phase", value: str(input.phase) });
        if (str(input.label)) fields.push({ label: "Label", value: str(input.label) });
        return base({
          label: "Update change",
          subject: ch || str(input.label) || str(input.phase),
          title: ch ? `Update change ${clip(ch, 40)}?` : "Update a change?",
          fields,
        });
      }

      case "change_request": {
        const ask = str(input.ask);
        const reason = str(input.reason);
        const ch = str(input.change);
        const fields: ApprovalField[] = [];
        if (ch) fields.push({ label: "Change", value: ch, mono: true });
        return base({
          label: ask === "merge" ? "Ask to merge" : ask === "fyr.deploy" ? "Ask to update Fyr" : "Request change",
          subject: reason || ch,
          title: ask === "merge" ? "Ask to merge a pull request?" : ask === "fyr.deploy" ? "Ask to update Fyr?" : "Request a change action?",
          summary: reason,
          fields,
        });
      }

      case "show": {
        const t = str(input.title);
        const fields: ApprovalField[] = [];
        if (str(input.path)) fields.push({ label: "Path", value: relPath(str(input.path), rawCwd, 72), mono: true });
        if (str(input.url)) fields.push({ label: "URL", value: str(input.url), mono: true });
        return base({
          label: "Show preview",
          subject: t || str(input.path) || str(input.url),
          title: t ? `Show “${clip(t, 48)}”?` : "Show a preview?",
          summary: str(input.caption),
          fields,
        });
      }

      case "view_publish":
      case "view_state":
      case "view_unchanged": {
        const st = (input.state ?? input.patch) as Record<string, unknown> | undefined;
        const headline = st && typeof st === "object" ? str(st.headline) : "";
        const reason = str(input.reason);
        const title = name === "view_publish" ? "Publish the live view?" : name === "view_unchanged" ? "Keep the live view as is?" : "Update the live view?";
        return base({
          label: "Update live view",
          subject: headline || reason,
          title,
          summary: headline || reason,
        });
      }

      case "thread_new": {
        const t = str(input.title);
        const text = str(input.text);
        const fields: ApprovalField[] = [];
        if (str(input.cwd)) fields.push({ label: "Folder", value: shortHome(str(input.cwd)), mono: true });
        if (text) fields.push({ label: "Message", value: text });
        return base({
          label: "Start thread",
          subject: t || firstLine(text, 72),
          title: t ? `Start thread “${clip(t, 48)}”?` : "Start a new thread?",
          fields,
        });
      }

      case "thread_send": {
        const text = str(input.text);
        const fields: ApprovalField[] = [];
        if (str(input.id)) fields.push({ label: "Thread", value: str(input.id), mono: true });
        if (text) fields.push({ label: "Message", value: text });
        return base({
          label: "Message thread",
          subject: firstLine(text, 72),
          title: "Send a message to another thread?",
          fields,
        });
      }

      case "thread_rename": {
        const t = str(input.title);
        return base({
          label: "Rename thread",
          subject: t,
          title: t ? `Rename thread to “${clip(t, 48)}”?` : "Regenerate thread title?",
        });
      }

      case "keep_process": {
        const why = str(input.why);
        const fields: ApprovalField[] = [];
        if (str(input.match)) fields.push({ label: "Process", value: str(input.match), mono: true });
        else if (typeof input.pid === "number") fields.push({ label: "PID", value: String(input.pid), mono: true });
        if (typeof input.hours === "number") fields.push({ label: "Duration", value: `${input.hours} h` });
        return base({
          label: "Keep process",
          subject: why || str(input.match),
          title: "Keep a background process running?",
          summary: why,
          fields,
        });
      }

      case "project_setup":
      case "host_setup": {
        const isHost = name === "host_setup";
        const g = genericFields(input, rawCwd, new Set(["ownerAsked"]));
        return base({
          label: isHost ? "Update host" : "Update project",
          subject: str(input.name) || str(input.project),
          title: isHost ? "Update this host’s appearance?" : "Update project settings?",
          fields: g.fields,
          hiddenFields: g.hidden,
        });
      }
    }
  }

  // Generic fallback for any other MCP or custom tool.
  const friendly = friendlyName(tool);
  const command = fa.cmd ?? "";
  const skip = new Set(["cwd", ...(command ? ["command"] : [])]);
  const g = genericFields(input, rawCwd, skip);
  const subject =
    str(input.title) ||
    str(input.label) ||
    str(input.why) ||
    str(input.query) ||
    str(input.url) ||
    shortPath ||
    str(input.name) ||
    firstLine(command || str(input.prompt) || str(input.message) || str(input.text), 72);
  return base({
    label: `Allow ${friendly}`,
    subject: firstLine(subject, 72),
    title: `Allow ${friendly}?`,
    command,
    fields: g.fields,
    hiddenFields: g.hidden,
    showWhere: Boolean(command),
  });
}
