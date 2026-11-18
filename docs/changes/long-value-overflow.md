# V0.long-value-overflow: a very long value fits every screen, on both fronts, and CI proves it

→ V0.17, V0.119, #84. (owner, 18 Nov: "CI-test all names etc for long value bugs in all screens for overflows".)

(1) **The test.** The layout test opens every phone screen and the main desktop screens in mock mode with `&long=word` (one unbroken run of 140 characters) and `&long=words` (a long sentence) added to every name, title, path, branch, repo, host, label and message. It fails on text outside its box, text cut with no ellipsis, a control cut off or squeezed, and a page that scrolls sideways.

(2) **The fixes.** An action's label, an ask, a link on a change card, a PR label, a PR or review author, a project's folder and the handover line wrap inside their box and keep every word. The right-hand value of a phone menu row ends in an ellipsis. On an inbox row a long project or provider name ends in an ellipsis at about a third of the line, so what follows it stays visible. The phone's chip row under a thread's title always fits its width.
