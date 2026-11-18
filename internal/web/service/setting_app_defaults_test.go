package service

import "testing"

// The clients page offers the app's sign-in QR code only while app access is on.
func TestDefaultSettingsCarryAppEnable(t *testing.T) {
	initHappTestDB(t)
	s := &SettingService{}

	for _, want := range []bool{true, false} {
		settings, err := s.GetAllSetting()
		if err != nil {
			t.Fatal(err)
		}
		settings.AppEnable = want
		if err := s.UpdateAllSetting(settings, SecretClears{}); err != nil {
			t.Fatal(err)
		}
		defaults, err := s.GetDefaultSettings("panel.example")
		if err != nil {
			t.Fatal(err)
		}
		values, ok := defaults.(map[string]any)
		if !ok {
			t.Fatalf("GetDefaultSettings type = %T, want map[string]any", defaults)
		}
		if got, ok := values["appEnable"].(bool); !ok || got != want {
			t.Fatalf("appEnable = %#v, want %t", values["appEnable"], want)
		}
	}
}
