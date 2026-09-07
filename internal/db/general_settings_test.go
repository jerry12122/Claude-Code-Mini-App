package db

import "testing"

func TestGeneralSettings_DefaultAndRoundtrip(t *testing.T) {
	database, err := Open(t.TempDir() + "/general_settings.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	got, err := database.GetGeneralSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.VscodeNoAdmin {
		t.Fatal("預設應為 false")
	}

	if _, err := database.PutGeneralSettings([]byte(`{"vscodeNoAdmin":true}`)); err != nil {
		t.Fatal(err)
	}
	got, err = database.GetGeneralSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !got.VscodeNoAdmin {
		t.Fatal("寫入後應為 true")
	}

	if _, err := database.PutGeneralSettings([]byte(`not json`)); err == nil {
		t.Fatal("非 JSON 應拒絕")
	}
}
