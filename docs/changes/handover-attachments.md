# handover-attachments

Cited as `V0.handover-attachments`.

A handover to an agent that reads the old conversation (rung 2) wrote only the
text of the owner's messages, so images and files the owner attached never
reached the new agent. `Engine.Conversation` now lists each attachment under
its message as `Attached: <name> (<type>) at <path>`, the path in Fyr's
attachment store, so the agent can Read it. A message with only attachments
is kept in the file. An attachment whose file is gone is marked as such.

No UI is touched, so desktop and phone are unchanged.
