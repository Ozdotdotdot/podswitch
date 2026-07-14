package config

import "testing"

func TestCurrentHeadset(t *testing.T) {
	t.Setenv("PODSWITCH_AIRPODS_MAC", "aa:bb:cc:dd:ee:ff")
	headset, err := CurrentHeadset()
	if err != nil {
		t.Fatal(err)
	}
	if headset.DevicePath != "/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF" || headset.PipeWireCard != "bluez_card.AA_BB_CC_DD_EE_FF" || headset.PipeWireSinkPrefix != "bluez_output.AA_BB_CC_DD_EE_FF" {
		t.Fatalf("unexpected headset identifiers: %#v", headset)
	}
}

func TestCurrentHeadsetRequiresMAC(t *testing.T) {
	t.Setenv("PODSWITCH_AIRPODS_MAC", "not-a-mac")
	if _, err := CurrentHeadset(); err == nil {
		t.Fatal("CurrentHeadset accepted an invalid MAC")
	}
}
