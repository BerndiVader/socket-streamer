package reo

type MdStateEnvelope []struct {
	Cmd   string `json:"cmd"`
	Code  int    `json:"code"`
	Value struct {
		State int `json:"state"`
	} `json:"value"`
}
