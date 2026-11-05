package api

type Provider struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Hint    string `json:"hint"`
	Group   string `json:"group"`
	Popular bool   `json:"popular"`
}

type AccountRef struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Drives int    `json:"drives"`
}

type Option struct {
	Name      string    `json:"name"`
	Help      string    `json:"help"`
	Type      string    `json:"type"`
	Default   string    `json:"default"`
	Required  bool      `json:"required"`
	Advanced  bool      `json:"advanced"`
	Secret    bool      `json:"secret"`
	Hide      bool      `json:"hide"`
	Exclusive bool      `json:"exclusive"`
	Examples  []Example `json:"examples"`
}

type Example struct {
	Value string `json:"value"`
	Help  string `json:"help"`
}

type Step struct {
	State   string            `json:"state"`
	Option  *Option           `json:"option"`
	Error   string            `json:"error"`
	Done    bool              `json:"done"`
	Service map[string]string `json:"service"`
}

type ProviderOptions struct {
	Type    string   `json:"type"`
	Options []Option `json:"options"`
}
