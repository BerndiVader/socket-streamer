package reo

type AiStateEnvelope []struct {
	Cmd   string `json:"cmd"`
	Code  int    `json:"code"`
	Value struct {
		Channel int `json:"channel"`
		DogCat  struct {
			AlarmState int `json:"alarm_state"`
			Support    int `json:"support"`
		} `json:"dog_cat"`
		Face struct {
			AlarmState int `json:"alarm_state"`
			Support    int `json:"support"`
		} `json:"face"`
		People struct {
			AlarmState int `json:"alarm_state"`
			Support    int `json:"support"`
		} `json:"people"`
		Vehicle struct {
			AlarmState int `json:"alarm_state"`
			Support    int `json:"support"`
		} `json:"vehicle"`
	} `json:"value"`
}
