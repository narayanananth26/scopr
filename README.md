# scopr

Launch Claude Code scoped to the repositories a task actually touches.

A _workspace_ is a directory holding several repositories. A _scope_ is a named
set of them. `scopr @surfaces` starts a session in the first repository of that
scope, with the rest passed as `--add-dir`.

## Install

```sh
go install ./cmd/scopr
```

## Use

```sh
scopr                             name, scope and prompt, step by step
scopr @surfaces                   start a session from a saved scope
scopr gl-panel gl-api             start one from repositories directly
scopr run list                    start one in a repository named like a command

scopr infer "fix the flaky build" suggest a scope for the task, then start
scopr list                        scopes in this workspace
scopr list --all                  scopes in every workspace, grouped
scopr show @surfaces              the repositories a scope names
scopr save @surfaces gl-panel gl-api
scopr rename @surfaces @edges
scopr delete @edges
scopr where                       the workspace you are in

scopr workspace add .             register a directory as a workspace
scopr workspace list
scopr workspace remove Goodlife
```

A scope is always written with `@`. The first repository in a scope becomes the
session's working directory, so order matters.

Flags may appear anywhere, and `--` ends them:

```sh
scopr save @web gl-webapp --workspace Goodlife
scopr --workspace Goodlife save @web gl-webapp
```

| flag                   |                                            |
| ---------------------- | ------------------------------------------ |
| `-w, --workspace NAME` | which workspace to resolve against         |
| `-p, --prompt TEXT`    | prompt to submit on start                  |
| `-l, --label TEXT`     | name the session                           |
| `-a, --all`            | every workspace, grouped                   |
| `--json`               | machine-readable output                    |
| `--verbose`            | with `infer`, show the survey's tool calls |

Run `scopr help <command>` for one command's usage and the flags it takes.

## Shell completions

```sh
scopr completion zsh
```

`completion` prints the script to stdout. zsh is the only shell scopr ships one
for. Candidates come from the binary, so the script follows the commands,
scopes, repositories, workspaces and per-command flags as they change.

For a temporary zsh session, load the script directly:

```sh
source <(scopr completion zsh)
```

For a persistent setup, write the generated `_scopr` function somewhere on your
fpath before compinit runs:

```sh
mkdir -p ~/.zfunc
scopr completion zsh > ~/.zfunc/_scopr
```

Then make sure your `.zshrc` contains:

```zsh
fpath=(~/.zfunc $fpath)
autoload -Uz compinit
compinit
```

If you already have a directory of your own on `fpath`, `print -l $fpath` will
show it: write `_scopr` there and skip the `.zshrc` change. Under oh-my-zsh,
which runs `compinit` for you, the `fpath` line has to go above
`source $ZSH/oh-my-zsh.sh`, and you should not add a `compinit` call.

Plugin managers can point at `completions/_scopr` in this repository, which is
the same file `scopr completion zsh` prints.

Nothing is installed for you and nothing writes to your shell configuration. A
missing or older scopr yields no completions rather than errors, so the script
and the binary can be updated independently. If the script is in place but
nothing completes, the cache is the usual cause: `rm -f ~/.zcompdump && exec zsh`.

### fzf

scopr ships plain zsh completions and nothing fzf-specific. If you already use
[fzf-tab](https://github.com/Aloxaf/fzf-tab) it renders any zsh completion menu
through fzf, including this one, with nothing to install. Candidates are
grouped, so each kind can have its own preview:

```zsh
zstyle ':fzf-tab:complete:scopr:scopes' fzf-preview 'scopr show ${word}'
zstyle ':fzf-tab:complete:scopr:repos'  fzf-preview 'git -C "$(scopr where | awk "{print \$2}")/${word}" log --oneline -10'
```

## Development

```sh
go test ./...
```
