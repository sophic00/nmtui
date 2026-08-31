package nm

type AccessPoint struct {
	InUse    bool
	SSID     string
	Chan     string
	Signal   int
	Security string
}

type SavedConnection struct {
	Name        string
	UUID        string
	Type        string
	Autoconnect string
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
