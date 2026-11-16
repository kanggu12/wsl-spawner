package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/kanggu12/wsl-spawner/internal/front/api"
	"github.com/kanggu12/wsl-spawner/internal/front/i18n"
	"github.com/kanggu12/wsl-spawner/internal/version"
)

// TestEveryHookStateStaysInTheHeader: the sign-out hook, in each of its
// states, stays beside the name on the main screen at every width from 40
// columns, the smallest the screen draws, in both languages, with a
// version as long as a release's: a hook that is not last, or not there,
// says so as long as one that is does.
func TestEveryHookStateStaysInTheHeader(t *testing.T) {
	was := version.Version
	version.Version = "v1.2.3"
	defer func() { version.Version = was }()
	defer i18n.Set(i18n.Lang("en"))
	for _, lang := range []string{"en", "ko"} {
		i18n.Set(i18n.Lang(lang))
		for _, h := range []struct {
			hook        api.Hook
			long, short string
		}{
			{api.Hook{Installed: true, Last: true}, "header.hook", "header.hook.short"},
			{api.Hook{Installed: true}, "header.hooknotlast", "header.hooknotlast.short"},
			{api.Hook{}, "header.nohook", "header.nohook.short"},
		} {
			for w := 40; w <= 160; w++ {
				m := mainWith(w, 30, 0, api.Distro{Name: "Ubuntu", Running: true, Kind: "stop-only", Version: 2})
				hook := h.hook
				m.hook = &hook
				head := ansi.Strip(m.header())
				if !strings.Contains(head, i18n.T(h.long)) && !strings.Contains(head, i18n.T(h.short)) {
					t.Errorf("%s, hook %s, %d columns: the hook left the header: %q", lang, h.long, w, head)
				}
				if ansi.StringWidth(head) > w {
					t.Errorf("%s, hook %s, %d columns: the header is %d wide: %q", lang, h.long, w, ansi.StringWidth(head), head)
				}
			}
		}
	}
}
