package nm

type AccessPoint struct {
	InUse    bool
	SSID     string
	BSSID    string
	Mode     string
	Chan     string
	Freq     string
	Rate     string
	Signal   int
	Security string
}

type SavedConnection struct {
	Name        string
	UUID        string
	Type        string
	Autoconnect string
	// SSID is the network this profile is for. It can differ from the
	// profile Name when a user renames a connection, so SSID is what
	// access points must be matched against.
	SSID string
}

type ActiveConnection struct {
	Name   string
	UUID   string
	Type   string
	Device string
}

type Status struct {
	WifiEnabled  bool
	State        string
	Connectivity string
}

type WifiState struct {
	Device string
	Active ActiveConnection
	IP     string
}
