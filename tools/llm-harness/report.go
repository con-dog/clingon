package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/con-dog/clingon/internal/llm"
)

// rating is a score out of 10 for how it went (objective: what happened) and
// how it felt (subjective), with the reasons.
type rating struct {
	Objective        int    `json:"objective"`
	ObjectiveReason  string `json:"objective_reason"`
	Subjective       int    `json:"subjective"`
	SubjectiveReason string `json:"subjective_reason"`
}

// phase is one part of the task, rated as it ends.
type phase struct {
	Name      string   `json:"name"`
	Completed bool     `json:"completed"`
	Actions   int      `json:"actions"` // tool calls (presses, typing, waits) since the phase before: a model may batch keys, so this varies by model
	Keys      int      `json:"keys"`    // keystrokes a person would make: each key pressed, each character typed
	Rating    rating   `json:"rating"`
	Notes     []string `json:"notes"`
}

// run is everything a run found, saved so runs can be compared.
type runRecord struct {
	Started    time.Time `json:"started"`
	Model      string    `json:"model"`
	Effort     string    `json:"effort"`
	Seconds    int       `json:"seconds"`
	Task       string    `json:"task"`
	Actions    int       `json:"actions"`
	Keys       int       `json:"keys"`
	Finished   bool      `json:"finished"`        // false: see Error
	Error      string    `json:"error,omitempty"` // why it stopped before finish: -max-steps, the model's API, …
	Usage      usage     `json:"usage"`
	Phases     []phase   `json:"phases"`
	Overall    *rating   `json:"overall,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	Notes      []string  `json:"ux_notes,omitempty"`
	Transcript string    `json:"transcript"`
}

// usage is what the run cost, summed over every turn.
type usage struct {
	Prompt     int     `json:"prompt_tokens"`
	Cached     int     `json:"cached_tokens"` // of Prompt, read from the cache
	Completion int     `json:"completion_tokens"`
	Cost       float64 `json:"cost"` // dollars, as OpenRouter charged it
}

func (in actInput) rating() rating {
	return rating{in.ObjectiveScore, in.ObjectiveReason, in.SubjectiveScore, in.SubjectiveReason}
}

// ratePhase records a phase's rating as it ends, and shows it.
func (a *agent) ratePhase(in actInput) {
	p := phase{Name: phaseName(in.Phase), Completed: in.Completed, Actions: a.steps - a.phaseFrom, Keys: a.keys - a.keysFrom, Rating: in.rating(), Notes: in.Notes}
	a.phaseFrom, a.keysFrom = a.steps, a.keys
	a.run.Phases = append(a.run.Phases, p)
	done := green("✓")
	if !p.Completed {
		done = yellow("✗")
	}
	fmt.Printf("\n%s %s %s  objective %s  subjective %s  %s\n", done, bold(p.Name), dim(fmt.Sprintf("(%d keys, %d actions)", p.Keys, p.Actions)),
		score(p.Rating.Objective), score(p.Rating.Subjective), dim(p.Rating.ObjectiveReason+" "+p.Rating.SubjectiveReason))
	for _, n := range p.Notes {
		fmt.Println(dim("    • ") + n)
	}
	fmt.Println()
	fmt.Fprintf(a.log, "\n### Phase: %s (%d keys, %d actions, completed: %v)\n\n- Objective %d/10: %s\n- Subjective %d/10: %s\n",
		p.Name, p.Keys, p.Actions, p.Completed, p.Rating.Objective, p.Rating.ObjectiveReason, p.Rating.Subjective, p.Rating.SubjectiveReason)
	for _, n := range p.Notes {
		fmt.Fprintf(a.log, "- Note: %s\n", n)
	}
	a.t.narrate(fmt.Sprintf("%s done · objective %d/10 · subjective %d/10", p.Name, p.Rating.Objective, p.Rating.Subjective), "")
}

// report prints the run's phases and overall rating, then saves the run.
func (a *agent) report() {
	r := &a.run
	r.Actions, r.Keys = a.steps, a.keys
	r.Seconds = int(time.Since(r.Started).Seconds())
	if len(r.Phases) > 0 {
		fmt.Println("\n" + bold("Phases"))
		for _, p := range r.Phases {
			mark := green("✓")
			if !p.Completed {
				mark = yellow("✗")
			}
			fmt.Printf("  %s %-16s %s  %s  %s\n", mark, p.Name, score(p.Rating.Objective), score(p.Rating.Subjective), dim(fmt.Sprintf("%d keys, %d actions", p.Keys, p.Actions)))
		}
	}
	if r.Overall != nil {
		fmt.Printf("\n%s\n%s\n\n%s\n", bold("Summary"), r.Summary, bold("UX notes"))
		for _, n := range r.Notes {
			fmt.Println("  • " + n)
		}
		fmt.Printf("\n%s  %s  %s\n%s  %s  %s\n",
			bold("Overall objective "), score(r.Overall.Objective), r.Overall.ObjectiveReason,
			bold("Overall subjective"), score(r.Overall.Subjective), r.Overall.SubjectiveReason)
		fmt.Fprintf(a.log, "\n## Overall\n\n%s\n\n- Objective %d/10: %s\n- Subjective %d/10: %s\n\n### UX notes\n\n",
			r.Summary, r.Overall.Objective, r.Overall.ObjectiveReason, r.Overall.Subjective, r.Overall.SubjectiveReason)
		for _, n := range r.Notes {
			fmt.Fprintf(a.log, "- %s\n", n)
		}
	}
	if r.Error != "" {
		fmt.Println("\n" + red("stopped: ") + r.Error)
	}
	fmt.Printf("\n%s%s · %d actions, %d keys · %s tokens in (%d%% cached), %s out · $%.2f\n", dim("run         "),
		time.Duration(r.Seconds)*time.Second, r.Actions, r.Keys, thousands(r.Usage.Prompt), percent(r.Usage.Cached, r.Usage.Prompt), thousands(r.Usage.Completion), r.Usage.Cost)
	fmt.Println(dim("transcript  ") + r.Transcript)
	if path, err := a.save(); err != nil {
		fmt.Println(yellow("couldn't save the run: ") + err.Error())
	} else {
		fmt.Println(dim("saved       ") + path + dim("   (compare runs: go run . -history)"))
	}
}

func (a *agent) save() (string, error) {
	if err := os.MkdirAll(a.o.runsDir, 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(a.run, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(a.o.runsDir, a.run.Started.Format("2006-01-02T15-04-05")+".json")
	return path, os.WriteFile(path, data, 0o644)
}

// history prints every saved run's ratings, oldest first: overall, then each
// phase's, objective/subjective, so a change's effect shows up.
func history(dir string) error {
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) == 0 {
		return fmt.Errorf("no runs saved in %s yet", dir)
	}
	sort.Strings(files)
	var runs []runRecord
	var names []string // every phase, in the order first seen
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var r runRecord
		if json.Unmarshal(data, &r) != nil {
			continue
		}
		runs = append(runs, r)
		for _, p := range r.Phases {
			if !slices.Contains(names, p.Name) {
				names = append(names, p.Name)
			}
		}
	}
	const col = 14 // as wide as the longest phase name
	cell := func(o, s int) string {
		if o == 0 && s == 0 {
			return fmt.Sprintf("%-*s", col, "  -")
		}
		return fmt.Sprintf("%-*s", col, fmt.Sprintf("%2d / %d", o, s))
	}
	head := fmt.Sprintf("%-14s %-12s %-6s %-7s %-*s", "run", "model", "keys", "cost", col, "overall")
	for _, n := range names {
		head += fmt.Sprintf("%-*s", col, short(n, col-1))
	}
	fmt.Println(bold(head))
	fmt.Println(dim("ratings are objective / subjective, out of 10"))
	for _, r := range runs {
		model := llm.ShortName(r.Model)
		cost := "-"
		if r.Usage.Cost > 0 {
			cost = fmt.Sprintf("$%.2f", r.Usage.Cost)
		}
		line := fmt.Sprintf("%-14s %-12s %-6d %-7s ", r.Started.Format("Jan 02 15:04"), short(model, 12), r.Keys, cost)
		switch {
		case r.Overall != nil:
			line += cell(r.Overall.Objective, r.Overall.Subjective)
		case r.Error != "":
			line += fmt.Sprintf("%-*s", col, "  stopped")
		default:
			line += cell(0, 0)
		}
		for _, n := range names {
			o, s := 0, 0
			for _, p := range r.Phases {
				if p.Name == n {
					o, s = p.Rating.Objective, p.Rating.Subjective
				}
			}
			line += cell(o, s)
		}
		fmt.Println(line)
	}
	return nil
}

// phaseName is a phase's name as the task gives it, so runs line up: a
// model may number it ("1. First look") or change its case.
func phaseName(name string) string {
	name = strings.TrimSpace(phaseNumber.ReplaceAllString(name, ""))
	for _, known := range []string{"First look", "Add a pack", "Run commands", "Recipe", "Create a pack", "Use your pack"} {
		if strings.EqualFold(name, known) {
			return known
		}
	}
	return name
}

var phaseNumber = regexp.MustCompile(`^\s*(phase\s*)?\d+\s*[.):-]?\s*`)

func short(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// thousands is n in thousands: "12.3k".
func thousands(n int) string { return fmt.Sprintf("%.1fk", float64(n)/1000) }

func percent(part, whole int) int {
	if whole == 0 {
		return 0
	}
	return part * 100 / whole
}

// score colours a mark out of 10: green from 8, amber from 5, red below.
func score(n int) string {
	s := fmt.Sprintf("%2d/10", n)
	switch {
	case n >= 8:
		return green(s)
	case n >= 5:
		return yellow(s)
	default:
		return red(s)
	}
}
