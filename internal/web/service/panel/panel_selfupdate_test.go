package panel

import (
	"errors"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
)

func TestGetUpdateInfoNeverOffersAnotherVersion(t *testing.T) {
	info, err := (&PanelService{}).GetUpdateInfo()
	if err != nil {
		t.Fatalf("GetUpdateInfo: %v, want no error because no release feed is consulted", err)
	}
	current := config.GetBaseVersion()
	want := PanelUpdateInfo{Channel: "stable", CurrentVersion: current, LatestVersion: current}
	if *info != want {
		t.Fatalf("GetUpdateInfo = %+v, want %+v", *info, want)
	}
}

func TestStartUpdateIsRefused(t *testing.T) {
	svc := &PanelService{}
	starts := map[string]func() (int64, error){
		"own channel":      svc.StartUpdate,
		"explicit channel": func() (int64, error) { return svc.StartUpdateChannel(true) },
	}
	for name, start := range starts {
		t.Run(name, func(t *testing.T) {
			runID, err := start()
			if !errors.Is(err, ErrSelfUpdateUnavailable) {
				t.Fatalf("err = %v, want ErrSelfUpdateUnavailable", err)
			}
			if runID != 0 {
				t.Fatalf("runID = %d, want 0: a refused update must not hand out a run to poll", runID)
			}
		})
	}
}
