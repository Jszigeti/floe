package icloud

import "testing"

func TestAllowDatalessDownloadsTurnsPolicyOn(t *testing.T) {
	orig := datalessPolicy()
	t.Cleanup(func() { _ = setDatalessPolicy(orig) })
	if err := setDatalessPolicy(datalessOff); err != nil {
		t.Fatal(err)
	}
	if err := AllowDatalessDownloads(); err != nil {
		t.Fatal(err)
	}
	if got := datalessPolicy(); got != datalessOn {
		t.Fatalf("policy = %d, want %d", got, datalessOn)
	}
}

func TestSetDatalessPolicyRejectsUnknownPolicy(t *testing.T) {
	if err := setDatalessPolicy(99); err == nil {
		t.Fatal("want error for unknown policy")
	}
}
