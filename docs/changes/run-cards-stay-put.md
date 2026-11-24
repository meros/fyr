# V0.run-cards-stay-put: a Run card stands where it was asked and never changes; asking again makes a new card

→ V0.run-cards-in-conversation. *Amends (1) and (2): the same id asked again replaced the request and moved its card to the newest place, and a card with no place in the loaded log stood at the end, under newer messages, so it looked stuck at the bottom.*

(1) **A request is always a new card.** `run_request` with an id that is taken (a card that stands, or one that ran) gets a number (`mount`, then `mount-2`), and its `run.requested` names that id. The agent never updates a card the owner already sees; its command, why and result stay as they were.

(2) **One place.** A card stands where `run.requested` named it and never moves, on the desktop and the phone.

(3) **No place in the log.** A request asked before this version, or further back than the loaded part of the log, stands before the loaded messages (under "Show earlier messages"), not at the end. It is the first stop for the "Needs you" jump.

(4) **What waits is in reach.** A request that waits for the owner stands in the dock above the composer, which is the one place to press; Show in conversation scrolls to its card.
