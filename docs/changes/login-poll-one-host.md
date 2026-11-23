# V0.login-poll-one-host: a one-host install's Settings follows a running login

→ V0.53 (login through Fyr), V0.127 (Settings of every host), issue #30.

Found on a fresh one-host install (the #199 run): **Log in** on a Claude account stayed on "Starting claude…" for good, with no link to open. The host had the link; the page never asked for it again.

(1) **Cause.** Settings polls a host's providers every 1.5 s while a login runs there. The effect was keyed on the names of the hosts with a running login, joined with spaces. With one host, its part has the name `""`, and `[""].join(" ")` is `""`, the same as no login at all, so the effect never ran again and no poll started. A front with several hosts names them, which is why the owner's own setup never showed it.

(2) **Fix.** The effect's key is the list itself (`JSON.stringify`), which tells `[]` from `[""]`.

(3) **Test.** The fixture server's `provider.login` now starts as the real CLI does: state `starting` with no link, then `waiting` with the link 0.8 s later. The layout test's login checks therefore pass only when the page follows a running login.
