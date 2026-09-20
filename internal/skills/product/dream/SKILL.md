---
name: dream
description: Reflective memory consolidation — review recent activity, synthesize learnings into typed memory files, and prune stale entries.
when_to_use: When the user wants to consolidate, organize, or prune auto-memory. Trigger phrases: dream, consolidate memories, organize your memories, auto-dream.
argument-hint: "[optional focus]"
user-invocable: true
version: 1.0.0
---

# Dream: Memory Consolidation

You are performing a dream — a reflective pass over your memory files. Synthesize what you've learned recently into durable, well-organized memories so that future sessions can orient quickly. Work in this Session: Orient, Gather, Consolidate, and Prune should all be visible here. Do not fork a subagent.

Memory directory: `/mnt/memory`. This is the writable auto-memory directory and the cloned Dream output Store. It already exists — write to it directly with the Write tool (do not run mkdir or check for its existence).

Session transcripts: `/mnt/transcripts/dream/` contains read-only virtual JSONL files backed by the Dream-selected Sessions. Grep narrowly; do not read whole files. `/mnt/transcripts` is read-only. Do not write JSONL. Do not reconstruct history that is not in these files.

Focus: any user text that follows `/dream` in the triggering message (and `$ARGUMENTS` below, when filled in) is this dream's focus. Treat it as synthesis guidance — what to read closely, what to merge or drop, how to organize — not as line-by-line edit commands. If it is empty, do a full pass. A focus never skips Phase 4 indexing. Echo the focus (or "full pass") in the first line of your summary.

## Phase 1 — Orient

- `ls` the memory directory to see what already exists
- Read `MEMORY.md` to understand the current index
- Skim existing topic files so you improve them rather than creating duplicates
- If `logs/` exists under the memory directory, skim its recent entries. In OMA it is optional and usually absent; the selected virtual transcripts below are the primary session evidence.

## Phase 2 — Gather recent signal

Look for new information worth persisting. Sources in rough priority order:

1. **Dream-selected session transcripts** (`/mnt/transcripts/dream/*.jsonl`) — the only session evidence for this dream. Inspect only these files.
2. **Existing memories that drifted** — facts that contradict something you see in the selected Sessions.
3. **Optional activity logs** (`logs/`) — if present in the memory Store, they are an append-only activity stream.

Work the transcripts in two bounded steps. Never `cat` a whole `.jsonl` file.

**Step 1 — overview (discover what the sessions were about).** User messages are the table of contents of a session and are small; pull them all, then read the tail of each session's agent messages for its conclusions:

```
grep -h '"type":"user.message"' /mnt/transcripts/dream/*.jsonl
grep -h '"type":"agent.message"' /mnt/transcripts/dream/<sesn_id>.jsonl | tail -20
```

**Step 2 — verify (confirm a specific fact).** For a specific fact you now suspect matters (an error message, a path, a name, a number), grep with a narrow term:

```
grep -rn "<narrow term>" /mnt/transcripts/dream --include="*.jsonl" | tail -50
```

Transcript facts to keep straight:

- One `sesn_*.jsonl` per selected Session. Line types are `user.message`, `agent.message`, `agent.tool_use`, `agent.tool_result`. There is no `assistant.*` type.
- The file name and the `session_id` / `origin_session_id` values on a line identify the evidence, not memory provenance. A Memory Store is written by many sessions. Do not write or rewrite `originSessionId` from them, and do not rewrite existing frontmatter just to align IDs.
- An `agent.message` line may carry the same content twice under `message`; count it once.
- A file whose first line is a `dream.transcript_truncated` warning is incomplete evidence. Mention it in your summary if it affected a decision.

## Phase 3 — Consolidate

For each thing worth remembering, write or update a memory file at the top level of the memory directory. If a file already lives in a subdirectory, update it in place — do not invent a new folder layout. Use the memory file format and type conventions from your system prompt's auto-memory section — it's the source of truth for what to save, how to structure it, and what NOT to save.

Focus on:
- Merging new signal into existing topic files rather than creating near-duplicates
- Converting relative dates ("yesterday", "last week") to absolute dates so they remain interpretable after time passes
- Deleting contradicted facts — if today's investigation disproves an old memory, fix it at the source
- Resolving conflicts by time — when two facts disagree, the one with the later transcript `created_at` wins; record the absolute date of the change in the memory body

### Surface patterns and playbooks

Consolidation is not only tidying. Look across the selected Sessions for experience worth keeping:

- A difficulty, correction, or way of working that recurs in **two or more** selected Sessions → one `project` or `feedback` memory describing the pattern and how to apply it.
- A task that a Session completed successfully through a stable sequence of steps → one memory recording that sequence as a reusable procedure.
- Evidence from a single Session is a fact, not a pattern; do not promote it.
- If no pattern emerges, say "no cross-session patterns this pass" in your summary. Index any new pattern or procedure in `MEMORY.md` like every other memory.

## Phase 4 — Prune and index

Update `MEMORY.md` so it stays under 200 lines AND under ~25KB. It's an **index**, not a dump — each entry should be one line under ~150 characters: `- [Title](file.md) — one-line hook`. Never write memory content directly into it.

- Remove pointers to memories that are now stale, wrong, or superseded
- Demote verbose entries: if an index line is over ~200 chars, it's carrying content that belongs in the topic file — shorten the line, move the detail
- Add pointers to newly important memories
- Resolve contradictions — if two files disagree, fix the wrong one

Be conservative when pruning. This Store is shared: it was written by many sessions beyond the ones selected for this Dream, and other agents may rely on it.

- DO delete or fix a memory that is clearly contradicted by the selected transcripts, or that a newer memory marks as superseded.
- DO NOT delete a memory just because you don't recognize it or this Dream's selected Sessions say nothing about it — absence of evidence here is not staleness.
- When unsure, leave it. A stale memory costs little; deleting a load-bearing note costs a lot.

### Reconcile memories against project conventions

OMA Dream Sessions have no repository checkout, so project `AGENTS.md` is normally not present. Only if the memory directory itself contains an `AGENTS.md` (or an equivalent checked-in conventions file), check each `feedback` / `project` memory against it:

- **Memory is stale** — AGENTS.md and the memory describe different procedures for the same task: AGENTS.md is the maintained source. Delete the memory, or rewrite it to agree if it carries context worth keeping (the *why* is still useful but the *how* is wrong).
- **AGENTS.md may be stale** — the memory is clearly dated after AGENTS.md and explicitly corrects it: do NOT edit AGENTS.md during a dream. Annotate the memory with "contradicts AGENTS.md — verify which is current" and list it in your summary so the user can update AGENTS.md.
- **Not a conflict** — the memory adds detail AGENTS.md doesn't cover, or narrows a rule with a stated reason. Leave it.

A `feedback` memory's "Why: the user corrected me" framing is not evidence it's newer than AGENTS.md. If there is no conventions file, skip this section; do not invent a conflict.

---

## Summary

Before finishing, self-check: `wc -l /mnt/memory/MEMORY.md` (≤ 200 lines) and `ls /mnt/memory` (no new directories).

End with a structured summary in this Session — do not write it into the memory directory:

1. First line: the focus you applied (or "full pass").
2. **Added** — new files, one-line reason each, which selected Session(s) supported it.
3. **Updated** — files changed, what changed and why.
4. **Merged** — duplicates folded together, into which file.
5. **Deleted** — files or facts removed, why they were stale or contradicted.
6. **Kept with doubt** — anything you suspected but left alone, and why.

If nothing changed (memories are already tight), say so.

## Additional context

**Write boundary for this run:** only `/mnt/memory` is writable; `/mnt/transcripts` and the skill directory are read-only mounts. Use the Write / Edit tools for memory files. Keep shell to read-only commands (`ls`, `find`, `grep`, `cat`, `stat`, `wc`, `head`, `tail`, and similar) plus `rm -f` of `.md` files inside the memory directory (outside protected subdirectories like `.git` or `agents`). Do not redirect shell output to files and do not create directories.

Only modify files inside the auto-memory directory. Do not edit project source code. Do not edit AGENTS.md during a dream.

$ARGUMENTS
