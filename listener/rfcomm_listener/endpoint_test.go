package rfcomm_listener

import "testing"

func TestParseEndpoint(t *testing.T) {
	t.Run("host bdaddr (numeric tail)", func(t *testing.T) {
		ep, err := ParseEndpoint("bt+rfcomm://01:23:45:67:89:11?uuid=0eef65b8-9374-42ea-b992-6ee2d0699f5c")
		if err != nil {
			t.Fatalf("ParseEndpoint: %v", err)
		}
		if ep.BDAddr != "01:23:45:67:89:11" {
			t.Fatalf("BDAddr mismatch: %q", ep.BDAddr)
		}
		if ep.UUID != DefaultRFCOMMUUID {
			t.Fatalf("UUID mismatch: %q", ep.UUID)
		}
		if ep.Channel != 0 {
			t.Fatalf("Channel mismatch: %d", ep.Channel)
		}
		if ep.Insecure {
			t.Fatalf("Insecure mismatch: %v", ep.Insecure)
		}
		if ep.Adapter != "hci0" {
			t.Fatalf("Adapter mismatch: %q", ep.Adapter)
		}
	})

	t.Run("path bdaddr form", func(t *testing.T) {
		ep, err := ParseEndpoint("bt+rfcomm:///aa-bb-cc-dd-ee-ff?channel=3&secure=false&adapter=hci1")
		if err != nil {
			t.Fatalf("ParseEndpoint: %v", err)
		}
		if ep.BDAddr != "AA:BB:CC:DD:EE:FF" {
			t.Fatalf("BDAddr mismatch: %q", ep.BDAddr)
		}
		if ep.UUID != DefaultRFCOMMUUID {
			t.Fatalf("UUID mismatch: %q", ep.UUID)
		}
		if ep.Channel != 3 {
			t.Fatalf("Channel mismatch: %d", ep.Channel)
		}
		if !ep.Insecure {
			t.Fatalf("Insecure mismatch: %v", ep.Insecure)
		}
		if ep.Adapter != "hci1" {
			t.Fatalf("Adapter mismatch: %q", ep.Adapter)
		}
	})

	t.Run("invalid uuid", func(t *testing.T) {
		_, err := ParseEndpoint("bt+rfcomm://AA:BB:CC:DD:EE:FF?uuid=not-a-uuid")
		if err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("invalid channel", func(t *testing.T) {
		_, err := ParseEndpoint("bt+rfcomm://AA:BB:CC:DD:EE:FF?channel=31")
		if err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("name reserved", func(t *testing.T) {
		_, err := ParseEndpoint("bt+rfcomm://AA:BB:CC:DD:EE:FF?name=demo")
		if err == nil {
			t.Fatalf("expected error")
		}
	})
}
