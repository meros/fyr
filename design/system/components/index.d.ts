// Fyr components. Props are documentation; the bundle is plain JS on window.Fyr.
export type State = "asking" | "failed" | "blocked" | "done" | "working" | "waiting" | "seen" | "snoozed" | "settled";
export interface ButtonProps { variant?: "default" | "primary" | "ghost" | "danger"; kbd?: string; disabled?: boolean; reason?: string; onClick?: () => void; children: any }
export interface KbdProps { children: string }
export interface StatusBadgeProps { state: State; label?: string }
export interface PRBadgeProps { number: number; state: "draft" | "open" | "pending" | "failed" | "passing" | "merged" | "closed" }
export interface HostChipProps { name: string; offline?: boolean }
export interface SectionHeaderProps { title: string; count?: number; kind?: "attention" | "plain"; open?: boolean; hint?: string; onToggle?: () => void }
export interface ThreadRowProps { title: string; state: State; reason?: string; summary?: string; pr?: PRBadgeProps; host?: string; offline?: boolean; time?: string; selected?: boolean; onClick?: () => void }
export interface DecisionCardProps { kind?: string; from: string; unblocks?: string; price?: string; questions: { text: string; multi?: boolean; options: { label: string; desc?: string }[] }[]; freeText?: boolean; defaultText?: string }
export interface ToastProps { title: string; detail?: string; action?: string; remaining?: number }
export interface PaletteProps { query?: string; groups: { title: string; items: { label: string; sub?: string; state?: State; host?: string; kbd?: string; selected?: boolean }[] }[] }
