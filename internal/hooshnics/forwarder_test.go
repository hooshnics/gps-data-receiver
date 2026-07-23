package hooshnics

import (
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"
)

func TestBuildRawRequestJSONPassthrough(t *testing.T) {
	payload := []byte(`[{"data":"+Hooshnic:V1.06,test"}]`)
	body, headers, err := buildRawRequest(payload, RawMeta{
		SourceType: "hooshnic",
		Encoding:   "json",
		ReceivedAt: time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(payload) {
		t.Fatalf("expected passthrough JSON, got %s", body)
	}
	if headers["Content-Type"] != "application/json" {
		t.Fatalf("unexpected content-type: %s", headers["Content-Type"])
	}
}

func TestBuildRawRequestTeltonikaHexEnvelope(t *testing.T) {
	frame := []byte{0x00, 0x00, 0x00, 0x00, 0x01, 0x02}
	body, headers, err := buildRawRequest(frame, RawMeta{
		IMEI:       "353201350783897",
		SourceType: "teltonika",
		Encoding:   "hex",
		ReceivedAt: time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if headers["Content-Type"] != "application/json" {
		t.Fatalf("unexpected content-type: %s", headers["Content-Type"])
	}

	var envelope map[string]interface{}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["imei"] != "353201350783897" {
		t.Fatalf("unexpected imei: %v", envelope["imei"])
	}
	if envelope["payload_encoding"] != "hex" {
		t.Fatalf("unexpected encoding: %v", envelope["payload_encoding"])
	}
	if envelope["source_type"] != "teltonika" {
		t.Fatalf("unexpected source_type: %v", envelope["source_type"])
	}
	want := hex.EncodeToString(frame)
	if envelope["payload"] != want {
		t.Fatalf("unexpected payload hex: %v want %s", envelope["payload"], want)
	}
}

func TestNewForwarderDisabled(t *testing.T) {
	f, err := NewForwarder(Config{Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if f != nil {
		t.Fatal("expected nil forwarder when disabled")
	}
}
