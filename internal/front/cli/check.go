package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/kanggu12/wsl-spawner/internal/front/api"

	"github.com/spf13/cobra"

	"github.com/kanggu12/wsl-spawner/internal/app"
	"github.com/kanggu12/wsl-spawner/internal/engine/check"
	"github.com/kanggu12/wsl-spawner/internal/engine/distros"
	"github.com/kanggu12/wsl-spawner/internal/engine/history"
	"github.com/kanggu12/wsl-spawner/internal/front/i18n"
)

// excludedPorts lists the ports Windows keeps from programs, TCP's and
// UDP's, as netsh lists them.
func excludedPorts(pc app.PC) ([]check.PortRange, error) {
	var all []check.PortRange
	for _, proto := range []string{"tcp", "udp"} {
		out, err := pc.ExcludedPorts(proto)
		if err != nil {
			return nil, err
		}
		all = append(all, check.ParseExcluded(out, proto)...)
	}
	return all, nil
}

// checkCmd looks over what wsl-spawner's work stands on.
func checkCmd(p *apps) *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "check",
		Short: "Look over what stopping and starting stand on: the sign-out hook, each distribution, sign-in, WSL, ports",
		Long: "check reads, and changes nothing: the sign-out hook and how long Windows waits for\n" +
			"it, each running distribution's systemd, how its last start ended, its\n" +
			"containers' stop timeouts and restart policies, the start at sign-in, WSL's own\n" +
			"settings, and the ports Windows keeps from programs. Each finding says what to do\n" +
			"about it. Exit code 3: something does not go as it should.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := p.get()
			if err != nil {
				return err
			}
			in, err := checkInputs(cmd.Context(), a)
			if err != nil {
				return err
			}
			gs := check.Run(in)
			out := cmd.OutOrStdout()
			if asJSON {
				if err := printJSON(out, checkJSON(gs)); err != nil {
					return err
				}
			} else {
				// The commands print English, for scripts.
				was := i18n.Current()
				i18n.Set(i18n.English)
				writeCheck(out, lookFor(out), gs)
				i18n.Set(was)
			}
			if check.Worst(gs) == check.Bad {
				return attention{fmt.Errorf("%s does not go as it should", counted(countMark(gs, check.Bad), "finding"))}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return c
}

// checkInputs reads everything the check looks at.
func checkInputs(ctx context.Context, a *app.App) (check.Inputs, error) {
	// Each probe is timed on the App's clock, which is Windows': the
	// distribution's clock is set beside it. The demo's distributions read
	// the demo's.
	ds, err := distros.ReadWithClock(ctx, a.WSL, a.Config, a.Now)
	if err != nil {
		return check.Inputs{}, err
	}
	info, _ := a.WSL.Info(ctx)
	in := check.Inputs{Config: a.Config, Distros: ds, WSLVersion: info.Version}
	in.Hook = hookFindings(a.PC)
	in.Logon = logonFindings(a)
	if home, err := a.PC.HomeDir(); err == nil {
		if b, err := a.PC.ReadFile(filepath.Join(home, ".wslconfig")); err == nil {
			in.WSLConfig = string(b)
		}
	}
	in.Excluded, in.ExcludedErr = excludedPorts(a.PC)
	in.LastStops = lastStops(a)
	return in, nil
}

// lastStops are wsl-spawner's newest stop of each distribution in the
// history; a history that cannot be read gives none.
func lastStops(a *app.App) map[string]check.LastStop {
	all, _ := history.Open(a.Dir).All()
	out := map[string]check.LastStop{}
	// Newest first: the first one met of each distribution is its last.
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].What != "stop" {
			continue
		}
		for _, d := range all[i].Distros {
			if _, ok := out[d.Name]; !ok {
				out[d.Name] = check.LastStop{Result: d.Result, At: all[i].At}
			}
		}
	}
	return out
}

// hookFindings is the sign-out hook as the check sees it, with the other
// scripts that stop WSL before it.
func hookFindings(pc app.PC) check.Hook {
	h, err := readHook(pc)
	if err != nil {
		return check.Hook{Err: err}
	}
	f := check.Hook{Installed: h.installed(), Last: h.last(), Count: len(h.Scripts), MaxWait: pc.MaxWait()}
	// The PowerShell sign-out scripts that run after the hook: by default,
	// or as psscripts.ini has them; none when it has them run first, and
	// none counted when they cannot be read.
	h.readPowerShell(pc)
	if h.powerShellAfter() {
		f.PowerShell = len(h.PowerShell)
		// Run after as psscripts.ini itself says, not as nothing set.
		f.PowerShellLast = h.PowerShellFirst != nil && !*h.PowerShellFirst
	}
	for i, s := range h.Scripts {
		// Those that run before wsl-spawner's, or all, with none of it.
		if i == h.Index || (h.installed() && i > h.Index) {
			continue
		}
		// Another copy's hook stops every distribution, then WSL, as this
		// one's does: when its program is there to start for this user. One
		// gone, or another account's, starts nothing here.
		if c, ok := h.copyAt(i); ok {
			// Before this copy's hook, it stops WSL first; with none of
			// this copy's, it is the one that runs.
			switch {
			case c.There && h.installed():
				f.StopsWSL = append(f.StopsWSL, s.CmdLine)
			case c.There:
				f.Copies = append(f.Copies, s.CmdLine)
			}
			continue
		}
		if b, err := pc.ReadFile(s.CmdLine); err == nil && len(b) < 1<<20 && check.StopsWSL(string(b)) {
			f.StopsWSL = append(f.StopsWSL, s.CmdLine)
		}
	}
	return f
}

// logonFindings is the sign-in program as the check sees it.
func logonFindings(a *app.App) check.Logon {
	cmdLine, ok, off, err := a.PC.LogonEntry(logonName)
	if err != nil {
		return check.Logon{Err: err}
	}
	l := check.Logon{Present: ok, Off: off, Command: cmdLine}
	if exe, err := a.PC.QuietExe(); err == nil {
		l.Want = `"` + exe + `" logon`
	}
	return l
}

// writeCheck prints the findings, group by group.
func writeCheck(out io.Writer, l look, gs []check.Group) {
	for i, g := range gs {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintln(out, l.bold(groupName(g)))
		for _, it := range g.Items {
			fmt.Fprintln(out, "  "+markOf(l, it.Mark)+" "+i18n.T(it.Key, it.Args...))
			if it.Do != "" {
				fmt.Fprintln(out, "    "+l.muted("→ "+i18n.T(it.Do, it.DoArgs...)))
			}
		}
	}
}

// nonNilAny is a list that prints as [] when empty, not null.
func nonNilAny(v []any) []any {
	if v == nil {
		return []any{}
	}
	return v
}

// groupName is a group's heading: a distribution's is its name.
func groupName(g check.Group) string {
	if g.Arg != "" {
		return i18n.T(g.Key, g.Arg)
	}
	return i18n.T(g.Key)
}

// markOf is a finding's mark, in its color.
func markOf(l look, m check.Mark) string {
	switch m {
	case check.OK:
		return l.clean("✓")
	case check.Warn:
		return l.warn("!")
	case check.Bad:
		return l.bad("✗")
	}
	return l.muted("·")
}

// countMark counts the findings of one mark.
func countMark(gs []check.Group, m check.Mark) int {
	n := 0
	for _, g := range gs {
		for _, it := range g.Items {
			if it.Mark == m {
				n++
			}
		}
	}
	return n
}

// markNames are the marks as JSON names them.
var markNames = map[check.Mark]string{check.OK: "ok", check.Info: "info", check.Warn: "warn", check.Bad: "bad"}

// checkJSON is the findings as JSON gives them, in English, the current
// language left as it is.
func checkJSON(gs []check.Group) api.Check {
	en := func(key string, args ...any) string { return i18n.In(i18n.English, key, args...) }
	o := api.Check{Schema: schema, Worst: markNames[check.Worst(gs)], Groups: []api.CheckGroup{}}
	for _, g := range gs {
		name := en(g.Key)
		if g.Arg != "" {
			name = en(g.Key, g.Arg)
		}
		gout := api.CheckGroup{Name: name, Key: strings.TrimPrefix(g.Key, "check.g."), Items: []api.Finding{}}
		for _, it := range g.Items {
			iout := api.Finding{Mark: markNames[it.Mark], Key: strings.TrimPrefix(it.Key, "check."), Text: en(it.Key, it.Args...),
				Args: nonNilAny(it.Args), DoArgs: nonNilAny(it.DoArgs)}
			if it.Do != "" {
				iout.Do, iout.DoKey = en(it.Do, it.DoArgs...), strings.TrimPrefix(it.Do, "check.")
			}
			gout.Items = append(gout.Items, iout)
		}
		o.Groups = append(o.Groups, gout)
	}
	return o
}
