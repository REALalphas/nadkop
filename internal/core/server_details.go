package core

import "encoding/json"

// Only public connection parameters reach the picker; addresses and credentials
// remain in the subscription payload. 0.0.0.0 rows are provider section headings.
func serverDetails(n Node) (transport string, port int, separator bool) {
	var stream struct {
		Network string `json:"network"`
	}
	_ = json.Unmarshal(n.StreamSettings, &stream)
	transport = stream.Network
	if transport == "" {
		transport = "tcp"
	}
	var settings struct {
		VNext []struct {
			Address string `json:"address"`
			Port    int    `json:"port"`
		} `json:"vnext"`
		Servers []struct {
			Address string `json:"address"`
			Port    int    `json:"port"`
		} `json:"servers"`
	}
	if json.Unmarshal(n.Settings, &settings) != nil {
		return transport, 0, false
	}
	address := ""
	if len(settings.VNext) > 0 {
		address, port = settings.VNext[0].Address, settings.VNext[0].Port
	} else if len(settings.Servers) > 0 {
		address, port = settings.Servers[0].Address, settings.Servers[0].Port
	}
	if port < 1 || port > 65535 {
		port = 0
	}
	return transport, port, address == "0.0.0.0"
}
