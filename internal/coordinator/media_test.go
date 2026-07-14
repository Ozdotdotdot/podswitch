package coordinator

import "testing"

func TestMediaActionValidation(t *testing.T) {
	for _, action := range []string{"volume-down", "volume-up", "previous", "next"} {
		if !isMediaAction(action) {
			t.Errorf("%q was rejected", action)
		}
	}
	if isMediaAction("shell") {
		t.Fatal("unsupported action was accepted")
	}
}
