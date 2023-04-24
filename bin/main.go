package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"

	"github.com/gorilla/websocket"
	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/codec/vpx"
	"github.com/pion/mediadevices/pkg/prop"
	"github.com/pion/webrtc/v3"

	_ "github.com/pion/mediadevices/pkg/driver/camera"
)

var peerConnection *webrtc.PeerConnection

func HTTPServer() {
	port := flag.Int("port", 8080, "http server port")
	flag.Parse()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		indexHTML, err := ioutil.ReadFile("index.html")
		if err != nil {
			panic(err)
		}
		homeTemplate := template.Must(template.New("").Parse(string(indexHTML)))
		homeTemplate.Execute(w, "wss://"+r.Host+"/ws")
	})

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(w, r)
	})

	err := http.ListenAndServe(":"+strconv.Itoa(*port), nil)
	if err != nil {
		panic(err)
	}
}

func serveWs(w http.ResponseWriter, r *http.Request) {
	type BaseMessage struct {
		Data  string `json:"data"`
		Topic string `json:"topic"`
	}

	upgrader := websocket.Upgrader{}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		panic(err)
	}

	go func() {
		defer conn.Close()

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				log.Println(err)
			}

			msgRes := BaseMessage{}
			switch json.Unmarshal(msg, &msgRes); msgRes.Topic {
			case "offer":
				if peerConnection != nil {
					return
				}

				config := webrtc.Configuration{
					ICEServers: []webrtc.ICEServer{
						{
							URLs: []string{"stun:stun.l.google.com:19302", "stun:stun.services.mozilla.com"},
						},
					},
				}

				// Create a new RTCPeerConnection
				openh264Params, err := vpx.NewVP8Params()
				if err != nil {
					panic(err)
				}
				fmt.Println(openh264Params.RTPCodec())
				openh264Params.BitRate = 100_000 // 500kbps

				codecSelector := mediadevices.NewCodecSelector(
					mediadevices.WithVideoEncoders(&openh264Params),
				)

				mediaEngine := webrtc.MediaEngine{}
				codecSelector.Populate(&mediaEngine)
				api := webrtc.NewAPI(webrtc.WithMediaEngine(&mediaEngine))
				peerConnection, err = api.NewPeerConnection(config)
				if err != nil {
					panic(err)
				}

				// Set the handler for ICE connection state
				// This will notify you when the peer has connected/disconnected
				peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
					fmt.Printf("Connection State has changed %s \n", connectionState.String())
				})

				peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
					if candidate != nil {
						w, err := conn.NextWriter(websocket.TextMessage)
						if err != nil {
							panic(err)
						}
						candidateMsg := BaseMessage{}
						candidateInit := webrtc.ICECandidateInit{}
						candidateInit.Candidate = candidate.ToJSON().Candidate
						candidateInit.SDPMLineIndex = candidate.ToJSON().SDPMLineIndex
						candidateInit.SDPMid = candidate.ToJSON().SDPMid
						candidateInit.UsernameFragment = candidate.ToJSON().UsernameFragment

						candidateByte, _ := json.Marshal(candidateInit)
						candidateMsg.Topic = "candidate"
						candidateMsg.Data = string(candidateByte)
						msgByte, _ := json.Marshal(candidateMsg)
						w.Write(msgByte)
					}
				})

				s, err := mediadevices.GetUserMedia(mediadevices.MediaStreamConstraints{
					Video: func(c *mediadevices.MediaTrackConstraints) {
						c.Height = prop.Int(480)
						c.Width = prop.Int(640)
					},
					Codec: codecSelector,
				})
				if err != nil {
					panic(err)
				}

				for _, track := range s.GetTracks() {
					fmt.Println(track.Kind())
					track.OnEnded(func(err error) {
						fmt.Printf("Track (ID: %s) ended with error: %v\n",
							track.ID(), err)
					})

					_, err = peerConnection.AddTransceiverFromTrack(track,
						webrtc.RtpTransceiverInit{
							Direction: webrtc.RTPTransceiverDirectionSendonly,
						},
					)
					if err != nil {
						panic(err)
					}
				}

				// Set the remote SessionDescription
				offer := webrtc.SessionDescription{}
				err = json.Unmarshal([]byte(msgRes.Data), &offer)
				log.Println(offer)
				if err != nil {
					log.Println(err)
				}
				err = peerConnection.SetRemoteDescription(offer)
				if err != nil {
					panic(err)
				}

				// Create an answer
				answer, err := peerConnection.CreateAnswer(nil)
				if err != nil {
					panic(err)
				}
				fmt.Println("ctx: create answer")
				// Sets the LocalDescription, and starts our UDP listeners
				err = peerConnection.SetLocalDescription(answer)
				if err != nil {
					panic(err)
				}
				fmt.Println(answer.SDP)

				answerRes := BaseMessage{}
				answerRes.Topic = "answer"
				answerSD, _ := json.Marshal(answer)
				answerRes.Data = string(answerSD)
				res, _ := json.Marshal(answerRes)

				w, err := conn.NextWriter(websocket.TextMessage)
				if err != nil {
					panic(err)
				}

				w.Write(res)

			case "candidate":
				log.Println("add candidate")
				candidate := webrtc.ICECandidateInit{}
				json.Unmarshal([]byte(msgRes.Data), &candidate)
				err := peerConnection.AddICECandidate(candidate)
				if err != nil {
					log.Println(err)
				}
			}
		}
	}()
}

func main() {
	HTTPServer()
}
