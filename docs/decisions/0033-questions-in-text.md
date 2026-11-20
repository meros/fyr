# 0033: Questions written in text reach Needs you through a cheap classifier

Status: accepted, 18 Nov 2026. Spec: V0.210.

## Problem

Needs you holds facts Fyr can compute: asks, failures, a ready PR. A question the agent only writes in its last message is none of these, so the thread lands in Unread and then Seen. In Fyr's own log for 14 to 18 Nov, formal asks were answered in a median of 1 minute (90% within 6). Turn endings that looked like a text question had a median of 5 minutes, and 10% took about 6 hours or more.

## What was measured

1,101 clean turn ends in 4.1 days had no ask and no live task or watch. A random 200 were labelled: 26 blocked on the owner (13%), 37 waiting on the owner somewhere else, 18 optional offers, 119 nothing. That is about 143 blocked turns, so the gap is real.

Three wordings were tested blind on the same 200 endings, with a Haiku-class model reading the last 500 characters:

| Wording | Yes | Blocked caught |
|---|---|---|
| "Does the agent need this person to reply or act before THIS piece of work can go on? An optional offer, a report, or something waiting elsewhere is no." | 54 | 22 of 26 |
| Narrow: reply in this conversation, and no for anything the app shows as a button or that concerns another thread | 20 | 13 of 26 |
| Broad: the next step of the agent's own work is waiting on the person, with examples | 69 | 26 of 26 |

The first is the shipped wording. The narrow one misses "paste this key" and "do you want a PR?". The broad one raises a quarter more rows. Regular expressions did worse: a wide one fired on 109 of 200 and caught 23, a narrow one fired on 50 and caught 18.

## Decision

At a clean turn end that could matter, a system job reads the end of the last message and returns whether the owner must act, and what. A yes puts the thread in Needs you with that line, marked as Fyr's reading, below all other causes.

- **Inferred, so it stays modest.** It sends no push, ranks last, and one press, Not a question, removes it. Raised and dismissed are logged, so its precision can be read from real use before anyone gives it a push.
- **Asynchronous and fail-open.** The call never delays a run's end. A failure leaves the thread as it is today.
- **Sticky.** 238 of the 1,101 endings were followed by a non-owner run within a minute. If such a run cleared the flag, rows would flicker, so only the owner's message, Not a question, settle or archive clear it.
- **The result is an observed input,** so the state is in the log and replays like every other fact.

## Rejected

- **A Stop hook that makes the agent declare.** An extra turn under 10 s costs a median of $0.115 and a mean of $0.28. On 1,101 turns that is about $130 to $310 in 4 days, it adds delay to every idle end, and it depends on the agent complying.
- **A new outcome tool with four states.** The existing ask, offer and watch tools already say these things. The gap is agents not using them for a plain question.
- **A Stuck state from the same call.** A 15-minute rule would have fired 25 times in 4.1 days, about half of them false.

## Limits

The labels come from a model, the sample is 200 endings clustered on 86 threads, and the test ran through an agent, not the production call. The numbers justify shipping behind a marker. They do not yet justify a push.
