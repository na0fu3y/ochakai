// `ochakai completion <shell>`: print a static completion script. The
// CLI deliberately has no flag framework (design doc 0004 §8), so the
// scripts are hand-written; TestCompletionScriptsStayInSync guards
// against drift from the real commands and flags.
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/na0fu3y/ochakai/internal/domain"
)

// typesMark stands in for the recommended vocabulary inside the three
// scripts below. Interpolating it beats writing the list out three more
// times: the vocabulary has one home, and narrowing it cannot leave a
// retired spelling behind in a shell (design doc 0038 §4.4). Each shell
// already wraps the whole list in one quote style, so the types that
// contain a space take the other one.
const typesMark = "@TYPES@"

// statusesMark is the same idea for the lifecycle vocabulary. It was
// written out three times until the vocabulary shrank to OKF's three
// values (design doc 0043 §3.2) and every copy had to be found by hand;
// no status contains a space, so one spelling serves all three shells.
const statusesMark = "@STATUSES@"

// trustsMark is the same arrangement for OKF's trust tiers (SPEC §5.3,
// design doc 0046 §3.10): one vocabulary, spelled in domain and
// interpolated here. No tier contains a space either.
const trustsMark = "@TRUSTS@"

var (
	zshCompletion  = expand(zshCompletionTmpl, `"`)
	bashCompletion = expand(bashCompletionTmpl, `'`)
	fishCompletion = expand(fishCompletionTmpl, `"`)
)

func expand(tmpl, quote string) string {
	names := make([]string, len(domain.Statuses))
	for i, st := range domain.Statuses {
		names[i] = string(st)
	}
	s := strings.ReplaceAll(tmpl, typesMark, domain.TypesQuoted(quote))
	s = strings.ReplaceAll(s, statusesMark, strings.Join(names, " "))
	return strings.ReplaceAll(s, trustsMark, strings.ReplaceAll(domain.TrustsHint(), ", ", " "))
}

func cmdCompletion(_ context.Context, args []string) error {
	fs := newBareFlagSet(
		"completion",
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

const zshCompletionTmpl = `#compdef ochakai
# zsh completion for ochakai. Either source <(ochakai completion zsh)
# in ~/.zshrc, or install it as an fpath file (no sourcing needed):
#   ochakai completion zsh > "${fpath[1]}/_ochakai"
_ochakai() {
  local -a commands
  commands=(
    'search:search knowledge; verified concepts rank higher'
    'browse:list one level of the ID hierarchy (folder view)'
    'context:the one-call read before a data question (full concepts)'
    'get:print one concept as an OKF document'
    'put:write a concept from OKF markdown or JSON, creating or replacing'
    'verify:append a verification (also re-affirms a verified concept)'
    'reject:record that a concept was reviewed and not accepted'
    'delete:soft-delete a concept'
    'purge:hard-delete a soft-deleted concept, freeing its id'
    'reembed:embed concepts that have no vector for the configured model'
    'move:move (rename) a concept; references are rewritten'
    'attach:attach files to a concept'
    'detach:remove an attachment'
    'usage:show usage totals for a concept'
    'stats:the whole loop: what is stored, what each queue holds, what review did'
    'report:report an outcome (worked/failed) for a concept'
    'revisions:list the change history of a concept (newest first)'
    'log:print the history under a path as OKF log.md'
    'export:download the knowledge base as an OKF bundle'
    'import:upload an OKF bundle'
    'use:pick the server for later commands'
    'whoami:print target server, identity, and reachability'
    'ui:serve the web UI locally, acting as you'
    'mcp-stdio:speak MCP on stdin/stdout, forwarding to the server'
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
        '*--type[filter by type]:type:(@TYPES@)' \
        '*--status[filter by status]:status:(@STATUSES@)' \
        '*--tag[filter by tag]:tag:' \
        '*--prefix[only concepts under this path]:prefix:' \
        '--source[only concepts citing this resource]:source:' \
        '--links-to[only concepts whose body links at this concept]:links-to:' \
        '*--trust[filter by who confirmed the concept (OKF SPEC §5.3)]:trust:(@TRUSTS@)' \
        '*--fm[filter by an OKF frontmatter key=value]:fm:' \
        '--rejected[only concepts a human turned down]' \
        '--sort[list instead of searching: by verification age, demand, failed reports, or declared expiry]:sort:(verified_at usage failed stale_after)' \
        '--limit[max results]:limit:' \
        '--cursor[resume a listing where the last page ended]:cursor:' \
        '--json[print the raw JSON response]' \
        '--url[server URL]:url:'
      ;;
    context)
      _arguments \
        '*--type[filter by type]:type:(@TYPES@)' \
        '*--status[filter by status]:status:(@STATUSES@)' \
        '*--tag[filter by tag]:tag:' \
        '*--prefix[only concepts under this path]:prefix:' \
        '*--trust[filter by who confirmed the concept (OKF SPEC §5.3)]:trust:(@TRUSTS@)' \
        '*--fm[filter by an OKF frontmatter key=value]:fm:' \
        '--limit[max full concepts]:limit:' \
        '--budget[stop rendering after ~bytes]:budget:' \
        '--json[print the raw JSON response]' \
        '--url[server URL]:url:'
      ;;
    get)
      _arguments '--json[print JSON instead of the OKF document]' '--download[save attachments into this directory]:directory:_files -/' '--url[server URL]:url:'
      ;;
    usage)
      _arguments '--json[print JSON]' '--url[server URL]:url:'
      ;;
    stats)
      _arguments \
        '--days[flow window in days, 1-180]:days:' \
        '*--prefix[measure only this subtree]:path:' \
        '--exit-code[exit 2 while any review queue is non-empty]' \
        '--json[print JSON]' \
        '--url[server URL]:url:'
      ;;
    browse)
      _arguments '--json[print the raw JSON response]' '--url[server URL]:url:'
      ;;
    revisions)
      _arguments '--limit[max results]:limit:' '--json[print the raw JSON response]' '--url[server URL]:url:'
      ;;
    log)
      _arguments '--limit[max concepts]:limit:' '--url[server URL]:url:'
      ;;
    report)
      _arguments '--note[context recorded with the report]:note:' '--json[print JSON]' '--url[server URL]:url:' '2:outcome:(worked failed)'
      ;;
    put)
      _arguments '-f[input file]:file:_files' '--only-if-new[write only if the id is free]' '--if-match[write only if the concept still has this version]:version:' '--json[print the concept as JSON]' '--url[server URL]:url:'
      ;;
    verify)
      _arguments '--json[print the concept as JSON]' '--url[server URL]:url:'
      ;;
    reject)
      _arguments '--note[why it was not accepted]:note:' '--withdraw[take back the rejection]' '--json[print the concept as JSON]' '--url[server URL]:url:'
      ;;
    delete|purge|detach|move)
      _arguments '--url[server URL]:url:'
      ;;
    attach)
      _arguments '--name[attachment name]:name:' '--json[print the attachment metadata as JSON]' '--url[server URL]:url:' '*:file:_files'
      ;;
    reembed)
      _arguments '--limit[max concepts per pass]:limit:' '--once[a single pass]' '--json[print JSON]' '--url[server URL]:url:'
      ;;
    export)
      _arguments '--no-attachments[export the markdown only]' '--url[server URL]:url:' '1:directory:_files -/'
      ;;
    import)
      _arguments '--dry-run[parse and list, write nothing]' '--strict[fail on any note or skip]' '--url[server URL]:url:' '1:bundle:_files'
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
    mcp-stdio)
      _arguments '--url[server URL]:url:'
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

const bashCompletionTmpl = `# bash completion for ochakai — source <(ochakai completion bash)
_ochakai() {
  local cur prev cmd opts
  cur=${COMP_WORDS[COMP_CWORD]}
  prev=${COMP_WORDS[COMP_CWORD-1]}
  cmd=${COMP_WORDS[1]}

  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=($(compgen -W "search browse context get put verify reject delete purge reembed move attach detach usage stats report revisions export import use whoami ui mcp-stdio completion serve serve-ui version help" -- "$cur"))
    return
  fi

  case $prev in
    --type|-type) COMPREPLY=($(compgen -W "@TYPES@" -- "$cur")); return ;;
    --status|-status) COMPREPLY=($(compgen -W "@STATUSES@" -- "$cur")); return ;;
    --trust|-trust) COMPREPLY=($(compgen -W "@TRUSTS@" -- "$cur")); return ;;
    --sort|-sort) COMPREPLY=($(compgen -W "verified_at usage failed stale_after" -- "$cur")); return ;;
    -f) compopt -o default 2>/dev/null; COMPREPLY=(); return ;;
  esac

  case $cmd in
    search)        opts="--type --status --tag --prefix --source --links-to --trust --fm --rejected --sort --limit --cursor --json --url" ;;
    browse)        opts="--json --url" ;;
    context)       opts="--type --status --tag --prefix --trust --fm --limit --budget --json --url" ;;
    get)           opts="--json --download --url" ;;
    usage)         opts="--json --url" ;;
    stats)         opts="--days --prefix --exit-code --json --url" ;;
    revisions)     opts="--limit --json --url" ;;
    log)           opts="--limit --url" ;;
    report)
      if [[ $prev != -* && $COMP_CWORD -eq 3 && $cur != -* ]]; then
        COMPREPLY=($(compgen -W "worked failed" -- "$cur"))
        return
      fi
      opts="--note --json --url" ;;
    put)           opts="-f --only-if-new --if-match --json --url" ;;
    verify)        opts="--json --url" ;;
    reject)        opts="--note --withdraw --json --url" ;;
    delete|purge|detach|move) opts="--url" ;;
    attach)        opts="--name --json --url" ;;
    reembed)       opts="--limit --once --json --url" ;;
    export)        opts="--url --no-attachments" ;;
    import)        opts="--dry-run --strict --url" ;;
    whoami)        opts="--json --url" ;;
    ui)            opts="--port --url" ;;
    mcp-stdio)     opts="--url" ;;
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

const fishCompletionTmpl = `# fish completion for ochakai — ochakai completion fish | source
complete -c ochakai -f

complete -c ochakai -n __fish_use_subcommand -a search -d 'search knowledge; verified concepts rank higher'
complete -c ochakai -n __fish_use_subcommand -a browse -d 'list one level of the ID hierarchy (folder view)'
complete -c ochakai -n __fish_use_subcommand -a context -d 'the one-call read before a data question (full concepts)'
complete -c ochakai -n __fish_use_subcommand -a get -d 'print one concept as an OKF document'
complete -c ochakai -n __fish_use_subcommand -a put -d 'write a concept from OKF markdown or JSON, creating or replacing'
complete -c ochakai -n __fish_use_subcommand -a verify -d 'append a verification (also re-affirms a verified concept)'
complete -c ochakai -n __fish_use_subcommand -a reject -d 'record that a concept was reviewed and not accepted'
complete -c ochakai -n __fish_use_subcommand -a delete -d 'soft-delete a concept'
complete -c ochakai -n __fish_use_subcommand -a purge -d 'hard-delete a soft-deleted concept, freeing its id'
complete -c ochakai -n __fish_use_subcommand -a reembed -d 'embed concepts that have no vector for the configured model'
complete -c ochakai -n __fish_use_subcommand -a move -d 'move (rename) a concept; references are rewritten'
complete -c ochakai -n __fish_use_subcommand -a attach -d 'attach files to a concept'
complete -c ochakai -n __fish_use_subcommand -a detach -d 'remove an attachment'
complete -c ochakai -n __fish_use_subcommand -a usage -d 'show usage totals for a concept'
complete -c ochakai -n __fish_use_subcommand -a stats -d 'the whole loop: what is stored, what each queue holds, what review did'
complete -c ochakai -n __fish_use_subcommand -a report -d 'report an outcome (worked/failed) for a concept'
complete -c ochakai -n __fish_use_subcommand -a revisions -d 'list the change history of a concept (newest first)'
complete -c ochakai -n __fish_use_subcommand -a log -d 'print the history under a path as OKF log.md'
complete -c ochakai -n __fish_use_subcommand -a export -d 'download the knowledge base as an OKF bundle'
complete -c ochakai -n __fish_use_subcommand -a import -d 'upload an OKF bundle'
complete -c ochakai -n __fish_use_subcommand -a use -d 'pick the server for later commands'
complete -c ochakai -n __fish_use_subcommand -a whoami -d 'print target server, identity, and reachability'
complete -c ochakai -n __fish_use_subcommand -a ui -d 'serve the web UI locally, acting as you'
complete -c ochakai -n __fish_use_subcommand -a mcp-stdio -d 'speak MCP on stdin/stdout, forwarding to the server'
complete -c ochakai -n __fish_use_subcommand -a completion -d 'print a shell completion script'
complete -c ochakai -n __fish_use_subcommand -a serve -d 'start the MCP + REST server'
complete -c ochakai -n __fish_use_subcommand -a serve-ui -d 'serve the team web UI as a deployed service'
complete -c ochakai -n __fish_use_subcommand -a version -d 'print the version'

complete -c ochakai -n '__fish_seen_subcommand_from search browse context get put verify reject delete purge reembed move attach detach usage stats report revisions log export import whoami ui mcp-stdio' -l url -x -d 'server URL'
complete -c ochakai -n '__fish_seen_subcommand_from ui' -l port -x -d 'port on 127.0.0.1'
complete -c ochakai -n '__fish_seen_subcommand_from import' -l dry-run -d 'parse and list, write nothing'
complete -c ochakai -n '__fish_seen_subcommand_from import' -l strict -d 'fail on any note or skip'
complete -c ochakai -n '__fish_seen_subcommand_from import' -F
complete -c ochakai -n '__fish_seen_subcommand_from search browse context get put verify reject reembed attach usage stats report revisions whoami' -l json -d 'print raw JSON'
complete -c ochakai -n '__fish_seen_subcommand_from stats' -l days -x -d 'flow window in days, 1-180'
complete -c ochakai -n '__fish_seen_subcommand_from stats' -l prefix -x -d 'measure only this subtree'
complete -c ochakai -n '__fish_seen_subcommand_from report' -l note -x -d 'context recorded with the report'
complete -c ochakai -n '__fish_seen_subcommand_from report' -a 'worked failed'
complete -c ochakai -n '__fish_seen_subcommand_from get' -l download -r -a '(__fish_complete_directories)' -d 'save attachments into this directory'
complete -c ochakai -n '__fish_seen_subcommand_from attach' -l name -x -d 'attachment name'
complete -c ochakai -n '__fish_seen_subcommand_from attach' -F
complete -c ochakai -n '__fish_seen_subcommand_from search context' -l type -x -a '@TYPES@' -d 'filter by type'
complete -c ochakai -n '__fish_seen_subcommand_from search context' -l status -x -a '@STATUSES@' -d 'filter by status'
complete -c ochakai -n '__fish_seen_subcommand_from search' -l sort -x -a 'verified_at usage failed stale_after' -d 'list instead of searching: by verification age, demand, failed reports, or declared expiry'
complete -c ochakai -n '__fish_seen_subcommand_from search context' -l tag -x -d 'filter by tag'
complete -c ochakai -n '__fish_seen_subcommand_from search context' -l prefix -x -d 'only concepts under this path'
complete -c ochakai -n '__fish_seen_subcommand_from stats' -l exit-code -d 'exit 2 while any review queue is non-empty'
complete -c ochakai -n '__fish_seen_subcommand_from search' -l source -x -d 'only concepts citing this resource'
complete -c ochakai -n '__fish_seen_subcommand_from search' -l links-to -x -d 'only concepts whose body links at this concept'
complete -c ochakai -n '__fish_seen_subcommand_from search context' -l trust -x -a '@TRUSTS@' -d 'filter by who confirmed the concept (OKF SPEC §5.3)'
complete -c ochakai -n '__fish_seen_subcommand_from search context' -l fm -x -d 'filter by an OKF frontmatter key=value'
complete -c ochakai -n '__fish_seen_subcommand_from search' -l rejected -d 'only concepts a human turned down'
complete -c ochakai -n '__fish_seen_subcommand_from search' -l cursor -x -d 'resume a listing where the last page ended'
complete -c ochakai -n '__fish_seen_subcommand_from reject' -l note -x -d 'why it was not accepted'
complete -c ochakai -n '__fish_seen_subcommand_from reject' -l withdraw -d 'take back the rejection'
complete -c ochakai -n '__fish_seen_subcommand_from search context revisions log' -l limit -x -d 'max results'
complete -c ochakai -n '__fish_seen_subcommand_from reembed' -l limit -x -d 'max concepts per pass'
complete -c ochakai -n '__fish_seen_subcommand_from reembed' -l once -d 'a single pass'
complete -c ochakai -n '__fish_seen_subcommand_from context' -l budget -x -d 'stop rendering after ~bytes'
complete -c ochakai -n '__fish_seen_subcommand_from put' -s f -r -F -d 'input file'
complete -c ochakai -n '__fish_seen_subcommand_from put' -l only-if-new -d 'write only if the id is free'
complete -c ochakai -n '__fish_seen_subcommand_from put' -l if-match -x -d 'write only if the concept still has this version'
complete -c ochakai -n '__fish_seen_subcommand_from use' -l name -x -d 'name to save the URL under'
complete -c ochakai -n '__fish_seen_subcommand_from use' -a '(ochakai use 2>/dev/null | cut -c3- | cut -f1)'
complete -c ochakai -n '__fish_seen_subcommand_from completion' -a 'zsh bash fish'
complete -c ochakai -n '__fish_seen_subcommand_from export' -l no-attachments -d 'export the markdown only'
complete -c ochakai -n '__fish_seen_subcommand_from export' -a '(__fish_complete_directories)'
`
