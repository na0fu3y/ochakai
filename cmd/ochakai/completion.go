// `ochakai completion <shell>`: print a static completion script. The
// CLI deliberately has no flag framework (design doc 0004 §8), so the
// scripts are hand-written; TestCompletionScriptsStayInSync guards
// against drift from the real commands and flags.
package main

import (
	"context"
	"fmt"
)

func cmdCompletion(_ context.Context, args []string) error {
	fs := newBareFlagSet(
		"Usage: ochakai completion <zsh|bash|fish>\n\nPrint a shell completion script. Load it with:\n\n  zsh:   source <(ochakai completion zsh)    # ~/.zshrc, after compinit\n  bash:  source <(ochakai completion bash)   # ~/.bashrc\n  fish:  ochakai completion fish | source    # ~/.config/fish/config.fish\n\nOr install it as a file the shell picks up by itself (what package\nmanagers do):",
		"  ochakai completion zsh > \"${fpath[1]}/_ochakai\"\n  ochakai completion fish > ~/.config/fish/completions/ochakai.fish\n")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	scripts := map[string]string{"zsh": zshCompletion, "bash": bashCompletion, "fish": fishCompletion}
	if len(pos) != 1 || scripts[pos[0]] == "" {
		fs.Usage()
		return errReported
	}
	fmt.Print(scripts[pos[0]])
	return nil
}

// Server names for `ochakai use <Tab>` come from the bare list output
// (name\turl per line, current marked in column 1-2): cut -c3- | cut -f1.

const zshCompletion = `#compdef ochakai
# zsh completion for ochakai. Either source <(ochakai completion zsh)
# in ~/.zshrc, or install it as an fpath file (no sourcing needed):
#   ochakai completion zsh > "${fpath[1]}/_ochakai"
_ochakai() {
  local -a commands
  commands=(
    'search:search knowledge; verified entries rank higher'
    'browse:list one level of the ID hierarchy (folder view)'
    'context:the one-call read before a data question (full entries)'
    'get:print one entry as an OKF document'
    'create:create an entry from OKF markdown or JSON'
    'update:replace an entry (kept as a revision)'
    'delete:soft-delete an entry'
    'move:move (rename) an entry; references are rewritten'
    'attach:attach files to an entry'
    'detach:remove an attachment'
    'usage:show usage totals for an entry'
    'report:report an outcome (worked/failed) for an entry'
    'revisions:list the change history of an entry (newest first)'
    'backlinks:list entries whose links point at this one'
    'compile:compile metrics into BigQuery SQL'
    'export:download the knowledge base as an OKF bundle'
    'import:upload an OKF bundle'
    'use:pick the server for later commands'
    'whoami:print target server, identity, and reachability'
    'ui:serve the web UI locally, acting as you'
    'completion:print a shell completion script'
    'serve:start the MCP + REST server'
    'serve-ui:serve the team web UI as a deployed service'
    'version:print the version'
    'help:show help'
  )
  if (( CURRENT == 2 )); then
    _describe -t commands 'ochakai command' commands
    return
  fi
  case $words[2] in
    search)
      _arguments \
        '*--type[filter by type]:type:(models metrics queries insights terms datasets tables references)' \
        '*--status[filter by status]:status:(draft verified deprecated rejected)' \
        '*--tag[filter by tag]:tag:' \
        '--sort[list instead of searching: by verification age or by demand]:sort:(verified_at usage)' \
        '--limit[max results]:limit:' \
        '--json[print the raw JSON response]' \
        '--url[server URL]:url:'
      ;;
    context)
      _arguments \
        '*--type[filter by type]:type:(models metrics queries insights terms datasets tables references)' \
        '*--status[filter by status]:status:(draft verified deprecated rejected)' \
        '*--tag[filter by tag]:tag:' \
        '--limit[max full entries]:limit:' \
        '--budget[stop rendering after ~bytes]:budget:' \
        '--min-score[drop hits below this score]:min-score:' \
        '--json[print the raw JSON response]' \
        '--url[server URL]:url:'
      ;;
    get)
      _arguments '--json[print JSON instead of the OKF document]' '--download[save attachments into this directory]:directory:_files -/' '--url[server URL]:url:'
      ;;
    usage)
      _arguments '--json[print JSON]' '--url[server URL]:url:'
      ;;
    browse)
      _arguments '--json[print the raw JSON response]' '--url[server URL]:url:'
      ;;
    revisions|backlinks)
      _arguments '--limit[max results]:limit:' '--json[print the raw JSON response]' '--url[server URL]:url:'
      ;;
    report)
      _arguments '--note[context recorded with the report]:note:' '--json[print JSON]' '--url[server URL]:url:' '2:outcome:(worked failed)'
      ;;
    create|update)
      _arguments '-f[input file]:file:_files' '--json[print the entry as JSON]' '--url[server URL]:url:'
      ;;
    delete|detach|move)
      _arguments '--url[server URL]:url:'
      ;;
    attach)
      _arguments '--name[attachment name]:name:' '--json[print the attachment metadata as JSON]' '--url[server URL]:url:' '*:file:_files'
      ;;
    compile)
      _arguments \
        '*--metric[metric name]:metric:' \
        '*--dimension[group-by column as dataset.field]:dimension:' \
        '*--filter[filter as "dataset.field op value"]:filter:' \
        '--grain[time grain as dataset.field\:grain]:grain:' \
        '--model[models entry id]:model:' \
        '--limit[LIMIT clause]:limit:' \
        '--json[print the full JSON response]' \
        '--url[server URL]:url:'
      ;;
    export)
      _arguments '--url[server URL]:url:' '1:directory:_files -/'
      ;;
    import)
      _arguments '--dry-run[parse and list, write nothing]' '--url[server URL]:url:' '1:bundle:_files'
      ;;
    use)
      local -a servers
      servers=(${(f)"$(ochakai use 2>/dev/null | cut -c3- | cut -f1)"})
      _arguments '--name[name to save the URL under]:name:' "1:server:(${servers[*]})"
      ;;
    whoami)
      _arguments '--json[print JSON]' '--url[server URL]:url:'
      ;;
    ui)
      _arguments '--port[port on 127.0.0.1]:port:' '--url[server URL]:url:'
      ;;
    completion)
      _arguments '1:shell:(zsh bash fish)'
      ;;
  esac
}
# Sourced: register with compdef. Autoloaded from fpath: this file runs
# as the function body, so call the (re)defined function directly.
if [ "$funcstack[1]" = "_ochakai" ]; then
  _ochakai
else
  compdef _ochakai ochakai
fi
`

const bashCompletion = `# bash completion for ochakai — source <(ochakai completion bash)
_ochakai() {
  local cur prev cmd opts
  cur=${COMP_WORDS[COMP_CWORD]}
  prev=${COMP_WORDS[COMP_CWORD-1]}
  cmd=${COMP_WORDS[1]}

  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=($(compgen -W "search browse context get create update delete move attach detach usage report revisions backlinks compile export import use whoami ui completion serve serve-ui version help" -- "$cur"))
    return
  fi

  case $prev in
    --type|-type) COMPREPLY=($(compgen -W "models metrics queries insights terms datasets tables references" -- "$cur")); return ;;
    --status|-status) COMPREPLY=($(compgen -W "draft verified deprecated rejected" -- "$cur")); return ;;
    --sort|-sort) COMPREPLY=($(compgen -W "verified_at usage" -- "$cur")); return ;;
    -f) compopt -o default 2>/dev/null; COMPREPLY=(); return ;;
  esac

  case $cmd in
    search)        opts="--type --status --tag --sort --limit --json --url" ;;
    browse)        opts="--json --url" ;;
    context)       opts="--type --status --tag --limit --budget --min-score --json --url" ;;
    get)           opts="--json --download --url" ;;
    usage)         opts="--json --url" ;;
    revisions|backlinks) opts="--limit --json --url" ;;
    report)
      if [[ $prev != -* && $COMP_CWORD -eq 3 && $cur != -* ]]; then
        COMPREPLY=($(compgen -W "worked failed" -- "$cur"))
        return
      fi
      opts="--note --json --url" ;;
    create|update) opts="-f --json --url" ;;
    delete|detach|move) opts="--url" ;;
    attach)        opts="--name --json --url" ;;
    compile)       opts="--metric --dimension --filter --grain --model --limit --json --url" ;;
    export)        opts="--url" ;;
    import)        opts="--dry-run --url" ;;
    whoami)        opts="--json --url" ;;
    ui)            opts="--port --url" ;;
    use)
      if [[ $cur != -* ]]; then
        COMPREPLY=($(compgen -W "$(ochakai use 2>/dev/null | cut -c3- | cut -f1)" -- "$cur"))
        return
      fi
      opts="--name" ;;
    completion)    COMPREPLY=($(compgen -W "zsh bash fish" -- "$cur")); return ;;
    *)             opts="" ;;
  esac

  if [[ $cur == -* ]]; then
    COMPREPLY=($(compgen -W "$opts" -- "$cur"))
  else
    compopt -o default 2>/dev/null
    COMPREPLY=()
  fi
}
complete -F _ochakai ochakai
`

const fishCompletion = `# fish completion for ochakai — ochakai completion fish | source
complete -c ochakai -f

complete -c ochakai -n __fish_use_subcommand -a search -d 'search knowledge; verified entries rank higher'
complete -c ochakai -n __fish_use_subcommand -a browse -d 'list one level of the ID hierarchy (folder view)'
complete -c ochakai -n __fish_use_subcommand -a context -d 'the one-call read before a data question (full entries)'
complete -c ochakai -n __fish_use_subcommand -a get -d 'print one entry as an OKF document'
complete -c ochakai -n __fish_use_subcommand -a create -d 'create an entry from OKF markdown or JSON'
complete -c ochakai -n __fish_use_subcommand -a update -d 'replace an entry (kept as a revision)'
complete -c ochakai -n __fish_use_subcommand -a delete -d 'soft-delete an entry'
complete -c ochakai -n __fish_use_subcommand -a move -d 'move (rename) an entry; references are rewritten'
complete -c ochakai -n __fish_use_subcommand -a attach -d 'attach files to an entry'
complete -c ochakai -n __fish_use_subcommand -a detach -d 'remove an attachment'
complete -c ochakai -n __fish_use_subcommand -a usage -d 'show usage totals for an entry'
complete -c ochakai -n __fish_use_subcommand -a report -d 'report an outcome (worked/failed) for an entry'
complete -c ochakai -n __fish_use_subcommand -a revisions -d 'list the change history of an entry (newest first)'
complete -c ochakai -n __fish_use_subcommand -a backlinks -d 'list entries whose links point at this one'
complete -c ochakai -n __fish_use_subcommand -a compile -d 'compile metrics into BigQuery SQL'
complete -c ochakai -n __fish_use_subcommand -a export -d 'download the knowledge base as an OKF bundle'
complete -c ochakai -n __fish_use_subcommand -a import -d 'upload an OKF bundle'
complete -c ochakai -n __fish_use_subcommand -a use -d 'pick the server for later commands'
complete -c ochakai -n __fish_use_subcommand -a whoami -d 'print target server, identity, and reachability'
complete -c ochakai -n __fish_use_subcommand -a ui -d 'serve the web UI locally, acting as you'
complete -c ochakai -n __fish_use_subcommand -a completion -d 'print a shell completion script'
complete -c ochakai -n __fish_use_subcommand -a serve -d 'start the MCP + REST server'
complete -c ochakai -n __fish_use_subcommand -a serve-ui -d 'serve the team web UI as a deployed service'
complete -c ochakai -n __fish_use_subcommand -a version -d 'print the version'

complete -c ochakai -n '__fish_seen_subcommand_from search browse context get create update delete move attach detach usage report revisions backlinks compile export import whoami ui' -l url -x -d 'server URL'
complete -c ochakai -n '__fish_seen_subcommand_from ui' -l port -x -d 'port on 127.0.0.1'
complete -c ochakai -n '__fish_seen_subcommand_from import' -l dry-run -d 'parse and list, write nothing'
complete -c ochakai -n '__fish_seen_subcommand_from import' -F
complete -c ochakai -n '__fish_seen_subcommand_from search browse context get create update attach usage report revisions backlinks compile whoami' -l json -d 'print raw JSON'
complete -c ochakai -n '__fish_seen_subcommand_from report' -l note -x -d 'context recorded with the report'
complete -c ochakai -n '__fish_seen_subcommand_from report' -a 'worked failed'
complete -c ochakai -n '__fish_seen_subcommand_from get' -l download -r -a '(__fish_complete_directories)' -d 'save attachments into this directory'
complete -c ochakai -n '__fish_seen_subcommand_from attach' -l name -x -d 'attachment name'
complete -c ochakai -n '__fish_seen_subcommand_from attach' -F
complete -c ochakai -n '__fish_seen_subcommand_from search context' -l type -x -a 'models metrics queries insights terms datasets tables references' -d 'filter by type'
complete -c ochakai -n '__fish_seen_subcommand_from search context' -l status -x -a 'draft verified deprecated rejected' -d 'filter by status'
complete -c ochakai -n '__fish_seen_subcommand_from search' -l sort -x -a 'verified_at usage' -d 'list instead of searching: by verification age or by demand'
complete -c ochakai -n '__fish_seen_subcommand_from search context' -l tag -x -d 'filter by tag'
complete -c ochakai -n '__fish_seen_subcommand_from search context revisions backlinks compile' -l limit -x -d 'max results / LIMIT clause'
complete -c ochakai -n '__fish_seen_subcommand_from context' -l budget -x -d 'stop rendering after ~bytes'
complete -c ochakai -n '__fish_seen_subcommand_from context' -l min-score -x -d 'drop hits below this score'
complete -c ochakai -n '__fish_seen_subcommand_from create update' -s f -r -F -d 'input file'
complete -c ochakai -n '__fish_seen_subcommand_from compile' -l metric -x -d 'metric name'
complete -c ochakai -n '__fish_seen_subcommand_from compile' -l dimension -x -d 'group-by column as dataset.field'
complete -c ochakai -n '__fish_seen_subcommand_from compile' -l filter -x -d 'filter as "dataset.field op value"'
complete -c ochakai -n '__fish_seen_subcommand_from compile' -l grain -x -d 'time grain as dataset.field:grain'
complete -c ochakai -n '__fish_seen_subcommand_from compile' -l model -x -d 'models entry id'
complete -c ochakai -n '__fish_seen_subcommand_from use' -l name -x -d 'name to save the URL under'
complete -c ochakai -n '__fish_seen_subcommand_from use' -a '(ochakai use 2>/dev/null | cut -c3- | cut -f1)'
complete -c ochakai -n '__fish_seen_subcommand_from completion' -a 'zsh bash fish'
complete -c ochakai -n '__fish_seen_subcommand_from export' -a '(__fish_complete_directories)'
`
