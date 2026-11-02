package config

import (
	"regexp"
	"strings"
	"testing"
)

// risky matches commands that delete, overwrite or throw away data;
// rewrite, merge or publish git history; change shared state (remotes,
// clusters, cloud, GitHub); stop or restart running things; or change the
// system with sudo. Every built-in command that matches must have a risk,
// so it asks before it runs.
var risky = regexp.MustCompile(strings.Join([]string{
	`(^|[;&|(] *)rm\b`, `-delete\b`, `\bp?kill\b`, `docker (rm|stop|restart)\b`, `system prune`,
	`git (clean|merge|rebase|cherry-pick|push|pull|reset|restore [^-])`, `git branch -[dD]`, `--amend`,
	`docker compose (down|up|restart)`,
	`tar -[cx]`, `rsync`, `curl [^|]*-\w*O`, `-X (POST|PUT|PATCH|DELETE)`, `http\.server`, `[^<=>&2]> *[^>&/ ]`, // not <> or =>
	`ssh-copy-id`, `aws s3 (sync|rm|mv)`, `aws configure --profile`,
	`gh (pr (merge|create)|repo create|issue create|run rerun|ssh-key add)`,
	`npm ci`, `uv sync`, `brew (uninstall|cleanup)`, `(brew|apt|dnf) upgrade$`, `-Syu`, `-Rs`, `\b(remove|autoremove)\b`,
	`sudo (systemctl|usermod)`, `git add -A`, `git commit -a`, `\buninstall\b`,
	// code
	`dotnet clean`, `dotnet publish`, `dotnet ef database (update|drop)`,
	`dotnet tool (install|update)`, `dev-certs https --trust`, `playwright install`, `--update-snapshots`,
	// databases
	`pg_(terminate|cancel)_backend`, `redis-cli [^|]*\b(expire|bgsave)\b`,
	`(?i)\bvacuum\b`, `psql [^|]* -[oif] `, `\bpsql\b[^|]* < `, `psql -d \S+ -c X$`, `\bcreate(db|user)\b`,
	`pg_dump`, `--rdb`, // read-only, but the file holds all the data
	// what other things install and change
	`brew (install|upgrade)\b`, `(apt|dnf) install`, `pacman -S `, `\b(npm|pnpm|bun) update\b`, `go get -u`,
	`gofmt -l -w`, `dotnet format`, `>> `, `claude update`, `\blogout\b`,
	`codex (exec|--sandbox|mcp add)`, `gpg\.format`,
}, "|"))

// deadly matches commands that destroy data or shared state for good, or can
// take down something live: they must be risk: danger, so you type a code before they run.
var deadly = regexp.MustCompile(strings.Join([]string{
	`git reset --hard`, `git clean`, `git restore [^-]`, `git push [^|]*(--delete|-d )`, `--force\b`,
	`(^|[;&|(] *)rm -r`, ` -delete( |$)`, `docker (rm|volume rm)`, `system prune`, `compose down [^|]*-v`,
	`aws s3 (sync|rm)`, `aws ecs (execute-command|update-service|stop-task)`, `aws ssm start-session`, `pg_terminate_backend`,
	`(?i)drop (database|table)`, `dropdb`, `flushall`, `flushdb`, `pg_restore`,
	`(?i)\btruncate\b`, `(?i)\bdelete from\b`, `psql [^|]* -f `, `git stash drop`, `pbpaste >`,
	`dropuser`, `redis-cli [^|]*\b(del|unlink)\b`,
	`dotnet ef database drop`, `dotnet ef database update [^-|;& ]`,
}, "|"))

// lessDeadly are deadly-looking commands that only destroy what can be
// made again, with why.
var lessDeadly = map[string]string{
	"node › Clean reinstall": "it only deletes node_modules, which npm install makes again",
}

var placeholder = regexp.MustCompile(`<[A-Za-z_][A-Za-z0-9_-]*>`)

func TestRiskyCommandsAreMarked(t *testing.T) {
	ps, err := Packs()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		c, err := loadPack(p.Name)
		if err != nil {
			t.Fatal(err)
		}
		for i := range c.Sections {
			for _, cmd := range c.Sections[i].All() {
				line := placeholder.ReplaceAllString(cmd.Cmd, "X")
				switch {
				case deadly.MatchString(line) && cmd.Risk != RiskDanger && lessDeadly[p.Name+" › "+cmd.Name] == "":
					t.Errorf("%s › %s: `%s` needs risk: danger", p.Name, cmd.Name, cmd.Cmd)
				case risky.MatchString(line) && cmd.Risk == "":
					t.Errorf("%s › %s: `%s` needs risk: warn", p.Name, cmd.Name, cmd.Cmd)
				}
			}
		}
	}
}
