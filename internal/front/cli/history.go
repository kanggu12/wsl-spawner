package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kanggu12/wsl-spawner/internal/engine/history"
)

// historyShown is how many entries history lists without --all.
const historyShown = 20

// historyCmd lists what wsl-spawner did, newest first, or clears it.
func historyCmd(p *apps) *cobra.Command {
	var asJSON, all, clear, yes bool
	c := &cobra.Command{
		Use:   "history",
		Short: "List the stops and starts wsl-spawner made, newest first",
		Long: "history lists each stop and start wsl-spawner made, by hand, as Windows signed out\n" +
			"or at sign-in, with how each distribution went. --clear removes them.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := p.get()
			if err != nil {
				return err
			}
			s := history.Open(a.Dir)
			out := cmd.OutOrStdout()
			if clear {
				if err := needAnswer(yes, "clear it"); err != nil {
					return err
				}
				if !yes {
					if err := confirm(cmd, "Remove the history? [y/N] ", "nothing removed"); err != nil {
						return err
					}
				}
				if err := s.Clear(); err != nil {
					return err
				}
				// With --json, what is left is said as JSON: no entries.
				if asJSON {
					return printJSON(out, historyJSON{schema, []history.Entry{}})
				}
				sayDone(out, "history removed")
				return nil
			}
			entries, err := s.All()
			if err != nil {
				return err
			}
			if asJSON {
				if entries == nil {
					entries = []history.Entry{}
				}
				return printJSON(out, historyJSON{schema, entries})
			}
			writeHistory(out, lookFor(out), entries, all)
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON, oldest first")
	c.Flags().BoolVar(&all, "all", false, "list every entry, not only the newest 20")
	c.Flags().BoolVar(&clear, "clear", false, "remove the history")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "with --clear, remove it without asking")
	return c
}

// historyJSON is the history as --json prints it.
type historyJSON struct {
	Schema  int             `json:"schema"`
	Entries []history.Entry `json:"entries"`
}

// writeHistory prints the entries as a table, newest first.
func writeHistory(out io.Writer, l look, entries []history.Entry, all bool) {
	if len(entries) == 0 {
		fmt.Fprintln(out, "no stop or start yet")
		return
	}
	rows := [][]string{{l.muted("WHEN"), l.muted("WHAT"), l.muted("BY"), l.muted("TOOK"), l.muted("DISTRIBUTIONS")}}
	shown := 0
	for i := len(entries) - 1; i >= 0 && (all || shown < historyShown); i-- {
		e := entries[i]
		rows = append(rows, []string{e.At.Local().Format("2006-01-02 15:04"), e.What + shutdownWord(e), byWords(e.By),
			fmt.Sprintf("%d s", int((time.Duration(e.Took*float64(time.Second))).Round(time.Second)/time.Second)), distrosWords(l, e)})
		shown++
	}
	_ = writeTable(out, []bool{false, false, false, true, false}, rows)
	if left := len(entries) - shown; left > 0 {
		fmt.Fprintln(out, l.muted(fmt.Sprintf("%d older: wsl-spawner history --all", left)))
	}
}

// shutdownWord marks a stop that stopped WSL after.
func shutdownWord(e history.Entry) string {
	if e.Shutdown {
		return " + WSL"
	}
	return ""
}

// byWords says who asked.
func byWords(by string) string {
	switch by {
	case "signout":
		return "sign-out"
	case "logon":
		return "sign-in"
	}
	return "by hand"
}

// distrosWords says how each distribution went, in its color.
func distrosWords(l look, e history.Entry) string {
	var parts []string
	for _, d := range e.Distros {
		w := d.Result
		switch d.Result {
		case "clean", "started", "running":
			w = l.clean(w)
		case "limit":
			w = l.warn("out of time")
		case "failed":
			w = l.warn("a step failed")
		case "error":
			w = l.bad("error")
		}
		parts = append(parts, d.Name+" "+w)
	}
	if e.Err != "" {
		parts = append(parts, l.bad(firstLine(e.Err)))
	}
	if len(parts) == 0 {
		return l.muted("nothing ran")
	}
	return strings.Join(parts, ", ")
}
