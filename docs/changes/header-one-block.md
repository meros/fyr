# V0.header-one-block: the thread header is the title, one row of chips and the tabs; a question in text stands under the newest message

→ V0.15, V0.21, V0.60, V0.210, V0.next-step-chips. *Amends V0.210 (5): the line "Fyr read this as a question" stands under the agent's newest message, not in the header.* (#60: in a 1259 × 676 window the header was four lines tall.)

(1) **The cause.** The header grew with what it held: a long title on two lines, the chips, the offers row with a result line per finished run, then the tabs. V0.next-step-chips moved the offers out; the text-ask line (V0.210) was the last block left between the chips and the tabs.

(2) **The header.** On desktop and phone the thread header holds the title (two lines at most), one row of chips, and the tabs. Nothing else stands in it. The title, the chips and the tabs share one left edge.

(3) **A question in text.** The line "Fyr read this as a question: …" with **Not a question** stands in the conversation under the agent's newest message, above the next-step chips: the message it reads is right above it. One component on both fronts; on the phone its button is 44 px tall (touch size).

(4) **The test.** `web/test/layout.e2e.mjs`, section V0.header-one-block: at 1259 × 676 (desktop) and 390 × 844 (phone), a thread with a 141-character title and a question in text. The title shows at most two lines; the desktop header is at most 149 px (14 + 2 × 26 + 12 + 24 + 12 + 34 + 1); the question line is in the conversation after the last message, not in the header.
