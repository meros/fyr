# V0.starting-moves: a thread whose agent is starting moves without the owner

→ V0.101, V0.153, V0.6. 

(1) **The cause.** A thread was Working only once its run began, and a run begins when Fyr writes the first message to a live agent. Starting the agent's process (the CLI, its MCP servers) can take up to a minute; in that time the thread had no run and fell through to Finished, unread.

(2) **The state.** A message to a thread whose agent is not running (a new thread, or a message after the run ended) marks the thread as starting. A starting thread is in Moving without you, with the reason "Starting". The mark ends when the run begins and when the start fails (the thread then fails as before). A process lost or a host restart during the start fails the thread like one during a run: the message never reached the agent, so the thread must not look finished. A wait for the machine (V0.153) still shows as that wait.
