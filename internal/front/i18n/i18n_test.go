package i18n

import (
	"strings"
	"testing"
)

func TestKoreanCallsAStopOneWay(t *testing.T) {
	// The screen calls wsl-spawner's stop 종료 (지난 종료, 다음 종료) and the
	// state 멈춤; a third word for the same stop reads as another thing.
	// Docker's own container states keep theirs: 일시 정지 is a pause.
	for key, text := range ko {
		// The container states are Docker's words, not wsl-spawner's stop.
		if strings.HasPrefix(key, "cstate.") {
			continue
		}
		// Any other 정지 is the stop under a second name.
		if strings.Contains(text, "정지") {
			t.Errorf("%s says 정지 for a stop: %q", key, text)
		}
	}
}
