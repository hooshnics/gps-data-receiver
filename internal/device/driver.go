package device

// Hints carry optional context for Detect/Decode (never required for binary AVL).
type Hints struct {
	IMEI        string
	SourceIP    string
	ContentType string
	Path        string
}

// Driver is a manufacturer-specific packet adapter.
// Teltonika CRC/parse algorithms must remain in codec/parser packages — drivers only wrap them.
type Driver interface {
	Name() string
	Detect(raw []byte, hints Hints) bool
	Validate(raw []byte) error
	Decode(raw []byte, hints Hints) (DecodeResult, error)
}

// DecodeResult holds normalized points plus optional opaque extras for archives.
type DecodeResult struct {
	DeviceType      string
	Protocol        string
	ProtocolVersion string
	Points          []PointView
}

// PointView is a minimal decode output to avoid an import cycle with telemetry.
// Callers map into telemetry.Point.
type PointView struct {
	IMEI      string
	Lat       float64
	Lng       float64
	Speed     int
	Direction int
	Ignition  bool
	Battery   *float64
	Voltage   *float64
	GSM       *int
	Satellite *int
	Timestamp string // RFC3339 UTC preferred; PiStat path may use local wall clock strings
	RawRef    string // optional hex / original fragment reference
}
