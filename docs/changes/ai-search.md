# V0.ai-search: search understands what the owner means, and the phone's search field is a real field

→ V0.108, V0.114, V0.115. (owner: "The search is useless... It's ugly on mobile and it should optimally use ai to find its way")

1. **The phone's field.** The Search screen's field has its own look: a 44 px rounded field with the icon, 16 px text (iOS zooms a page on a smaller field), a clear button and the keyboard's Search key.
2. **Typed search takes words.** Every word of the query must be in the title, the project or `#<PR number>`, in any order. It stays instant and costs nothing.
3. **Ask AI.** With text in the field, desktop and phone offer "Ask AI to find …"; the button, Enter on the desktop and the keyboard's Search key on the phone start it. It runs only on that act, since each run is a model call. The answer lists at most 12 threads per host, best first, each with what in it fits; a press opens the thread. Changing the text or Cancel ends the call.
4. **What the model reads.** `POST /api/search` on each host with threads to search: title, project, branch, folder, and the start and the latest part of the conversation of at most 120 threads per host. The client sends only the threads search may reach, so a quiet project's threads stay out unless the unlock runs (V0.114). The conversation is data for the model; its answer can only name threads it was given.
5. **System agent.** The call is the new system job `search` ("Thread search", V0.115): Haiku on the host's default provider unless the owner picks another in Settings. A host runs at most two at once. A host that fails does not hide the other hosts' answers; the panel says how many did not answer.
