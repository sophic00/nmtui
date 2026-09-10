//go:build live

// Smoke test against the real NetworkManager. Run manually with:
//
//	go test -tags live -run TestLive ./internal/nm/
//
// It only issues read-only nmcli commands.
package nm

import (
	"context"
	"testing"
)

func TestLiveWifiCommands(t *testing.T) {
	ctx := context.Background()
	c := NewClient()

	st, err := c.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	t.Logf("status: %+v", st)

	w, err := c.GetWifiState(ctx, "")
	if err != nil {
		t.Fatalf("GetWifiState: %v", err)
	}
	t.Logf("wifi: %+v", w)

	aps, err := c.ListAccessPoints(ctx, false, "")
	if err != nil {
		t.Fatalf("ListAccessPoints: %v", err)
	}
	t.Logf("access points: %d", len(aps))
	if len(aps) > 0 {
		t.Logf("first ap: %+v", aps[0])
	}

	saved, err := c.ListSaved(ctx)
	if err != nil {
		t.Fatalf("ListSaved: %v", err)
	}
	for _, s := range saved {
		t.Logf("saved: name=%q ssid=%q autoconnect=%q", s.Name, s.SSID, s.Autoconnect)
	}

	if w.Device != "" {
		w2, err := c.GetWifiState(ctx, w.Device)
		if err != nil {
			t.Fatalf("GetWifiState(%s): %v", w.Device, err)
		}
		t.Logf("wifi explicit: %+v", w2)
	}
}
