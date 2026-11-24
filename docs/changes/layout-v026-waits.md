# V0.layout-v026-waits: the V0.26 layout checks wait for the focus the keyboard leads to

The ⋯ menu check and the Project… picker check read the page right after a key, but the focus moves into the menu or the picker's search in an effect after the key's render. On a slow page the read landed between the two and failed.

1. Both checks now wait for the state itself (the focus inside the menu, the focus on the picker's search) before they read it.
2. Found with the V0.26 and V0.dialogs-review round 2 sections run together, 12 runs beside 8 busy loops: 2 failed before. After the change 20 of 20 runs passed under the same load.
3. Not found: the cause of "a reopened New thread shows no line about its draft". It failed once in that run and not in 25 runs of its own section under the same load, so it stays open; nothing here claims it fixed.
