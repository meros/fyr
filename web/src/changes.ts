// The Changes tab (SPEC 0.5 V0.25): pure logic. Not implemented yet.
export interface ChangedFile {
  path: string;
  oldPath?: string;
  status: "added" | "modified" | "deleted" | "renamed";
  additions: number;
  deletions: number;
  binary: boolean;
}
export interface ChangesDTO {
  git: boolean;
  root?: string;
  branch?: string;
  base?: string;
  mergeBase?: string;
  commits?: number;
  files?: ChangedFile[];
  truncated?: boolean;
}
export interface DiffLine {
  kind: "meta" | "hunk" | "ctx" | "add" | "del" | "note";
  text: string;
  old?: number;
  new?: number;
}
export function parsePatch(_patch: string): DiffLine[] {
  throw new Error("todo");
}
export function shortenPath(_path: string, _max: number): string {
  throw new Error("todo");
}
export function sortFiles(_files: ChangedFile[]): ChangedFile[] {
  throw new Error("todo");
}
export function statusMark(_s: string): string {
  throw new Error("todo");
}
export function summaryLine(_c: ChangesDTO): string {
  throw new Error("todo");
}
