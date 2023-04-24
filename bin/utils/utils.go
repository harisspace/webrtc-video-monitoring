package utils

type BaseMessage struct {
	Topic string                 `json:"topic"`
	Data  map[string]interface{} `json:"data,omitempty"`
}

type SessionDescription struct {
	Sdp  string `json:"sdp"`
	Type string `json:"type"`
}

type DataMsgOffer struct {
	Sdp string `json:"sdp"`
}

type DataMsgAnswer struct {
	Sdp string `json:"sdp"`
}
