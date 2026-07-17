# Claude Code integration

Two layers, weakest to strongest:

1. **[CLAUDE.md](CLAUDE.md)** teaches the agent the commands and the
   write-learnings-back habit. Copy its contents into your project's
   `CLAUDE.md`. Instructions rely on the agent remembering them — good
   baseline, imperfect adherence.
2. **[hooks/](hooks)** make the loop automatic. Hooks are executed by
   Claude Code itself, so they fire every time, no agent judgment
   involved — the same trick memory layers use, minus the LLM:
   - `ochakai-recall.sh` (**UserPromptSubmit**) runs
     `ochakai context "<prompt>"` and injects the resulting pack —
     full entries behind the top hits, links expanded — into the
     context before the agent starts working. Automatic recall.
   - `ochakai-write-back.sh` (**Stop**) interrupts the agent once per
     data-work session, right before it stops, and asks it to save
     reusable queries and metric insights (or explicitly decide there
     is nothing worth saving). Automatic write-back prompting.

## Install

```sh
# once per machine: point the CLI at your server
ochakai use https://ochakai-<hash>.run.app

# per project
mkdir -p .claude/hooks
cp hooks/ochakai-*.sh .claude/hooks/
chmod +x .claude/hooks/ochakai-*.sh
# merge the "hooks" key of settings.json into .claude/settings.json
```

Both scripts need `jq` and fail silently: an unreachable knowledge base
never blocks a prompt or a stop.

## Tuning

| Env var | Effect |
|---|---|
| `OCHAKAI_RECALL_BUDGET` | Max bytes the recall hook injects (default 4000) |
| `OCHAKAI_RECALL_MIN_SCORE` | Drop hits below this score before injecting (default 0 = inject whenever anything matches) |

Search scores are **not calibrated**: they depend on the server's search
mode (trigram vs hybrid embeddings) and on your corpus — English trigram
matches score far higher than Japanese ones. Before setting a floor, run
`ochakai search "<typical prompt>" | cut -f1-2` for a few relevant and
irrelevant prompts and pick a value that separates them; there is no
universal default, which is why the hook ships with the floor off.

The recall hook skips slash commands. The write-back hook only fires in
sessions whose transcript looks like data work (SQL, ochakai usage) and
at most once per session; it never fires twice in a row thanks to
`stop_hook_active`.
