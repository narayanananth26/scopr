# scopr

Launch Claude Code scoped to the repositories a task actually touches.

A _workspace_ is a directory holding several repositories. A _scope_ is a named
set of them. `scopr @surfaces` starts a session in the first repository of that
scope, with the rest passed as `--add-dir`.

## Install

```sh
brew tap narayanananth26/scopr
brew trust narayanananth26/scopr
brew install scopr
```

Homebrew refuses to load formulae from a third-party tap until you trust it,
which is what the middle line is for.

Or from a clone:

```sh
go install ./cmd/scopr
```

scopr runs Claude Code, so `claude` has to be on your `PATH`.

## Use

```sh
scopr                             name, scope and prompt, step by step
scopr @surfaces                   start a session from a saved scope
scopr gl-panel gl-api             start one from repositories directly
scopr run list                    start one in a repository named like a command

scopr infer "fix the flaky build" suggest a scope for the task, then start
scopr list                        scopes in every workspace, grouped
scopr list -w Goodlife            scopes in one workspace
scopr show @surfaces              the repositories a scope names
scopr save @surfaces gl-panel gl-api
scopr rename @surfaces @edges
scopr delete @edges
scopr where                       the workspace you are in

scopr workspace add .             register a directory as a workspace
scopr workspace list
scopr workspace remove Goodlife   forget one and delete its scopes
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
| `--json`               | machine-readable output                    |
| `--force`              | with `workspace remove`, skip the question |
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

Installing through Homebrew puts `_scopr` in
`$(brew --prefix)/share/zsh/site-functions` for you, so there is no file to
write. That directory still has to be on your fpath, which is what Homebrew's
own setup line does:

```zsh
eval "$(brew shellenv)"
```

If `brew`, `gh` or `cargo` completions do not work either, that line is the one
that is missing.

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
