package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/codec/opus"
	"github.com/pion/mediadevices/pkg/codec/vpx"
	"github.com/pion/mediadevices/pkg/prop"
	"github.com/pion/webrtc/v3"

	"github.com/stianeikeland/go-rpio"

	_ "github.com/pion/mediadevices/pkg/driver/audiotest"
	_ "github.com/pion/mediadevices/pkg/driver/camera"
)

var (
	peerConnection  *webrtc.PeerConnection
	peerConnections []peerConnectionState
	pin1                   = rpio.Pin(18)
	servoDegree     uint32 = 0
)

type peerConnectionState struct {
	peerConnection *webrtc.PeerConnection
	websocket      *threadSafeWriter
}

// Helper to make Gorilla Websocket threadsafe
type threadSafeWriter struct {
	*websocket.Conn
	sync.Mutex
}

func (t *threadSafeWriter) WriteJSON(v interface{}) error {
	t.Lock()
	defer t.Unlock()

	return t.Conn.WriteJSON(v)
}

func HTTPServer() {
	port := flag.Int("port", 8080, "http server port")
	flag.Parse()

	fs := http.FileServer(http.Dir("./static"))

	http.Handle("/static/", http.StripPrefix("/static", fs))

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		indexHTML, err := ioutil.ReadFile("index.html")
		if err != nil {
			panic(err)
		}
		homeTemplate := template.Must(template.New("").Parse(string(indexHTML)))
		homeTemplate.Execute(w, "ws://"+r.Host+"/ws")
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
	unsafeConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		panic(err)
	}

	c := &threadSafeWriter{unsafeConn, sync.Mutex{}}

	defer c.Close()

	for {
		_, msg, err := c.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Println(err)
			}
			break
		}

		msgRes := BaseMessage{}

		switch json.Unmarshal(msg, &msgRes); msgRes.Topic {
		case "offer":
			config := webrtc.Configuration{
				ICEServers: []webrtc.ICEServer{
					{
						URLs: []string{"stun:stun.l.google.com:19302", "stun:stun.services.mozilla.com"},
					},
				},
			}

			// Create a new RTCPeerConnection
			// Register video codec
			vp8Params, err := vpx.NewVP8Params()
			if err != nil {
				panic(err)
			}
			fmt.Println(vp8Params.RTPCodec())
			vp8Params.BitRate = 400_000 // 500kbps

			// Register audio codec
			opusParams, err := opus.NewParams()
			if err != nil {
				panic(err)
			}

			codecSelector := mediadevices.NewCodecSelector(
				mediadevices.WithVideoEncoders(&vp8Params),
				mediadevices.WithAudioEncoders(&opusParams),
			)

			mediaEngine := webrtc.MediaEngine{}
			codecSelector.Populate(&mediaEngine)
			api := webrtc.NewAPI(webrtc.WithMediaEngine(&mediaEngine))
			peerConnection, err = api.NewPeerConnection(config)
			if err != nil {
				panic(err)
			}

			// Register data channel creation handling
			peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
				fmt.Printf("New DataChannel %s %d\n", d.Label(), d.ID())

				// Register channel opening handling
				d.OnOpen(func() {
					fmt.Printf("Data channel '%s'-'%d' open\n", d.Label(), d.ID())
				})

				// Register text message handling
				d.OnMessage(func(msg webrtc.DataChannelMessage) {
					fmt.Println(string(msg.Data))

					switch string(msg.Data) {
					case "go-right":
						servoDegree += 36
						if servoDegree >= 162 {
							break
						}
						pin1.DutyCycle(2+(servoDegree/18), 12)
						time.Sleep(time.Millisecond * 50)
						pin1.DutyCycle(0, 12)
						time.Sleep(time.Millisecond * 50)
					case "go-left":
						servoDegree -= 36
						if servoDegree <= 0 {
							break
						}

						pin1.DutyCycle(2+(servoDegree/18), 12)
						time.Sleep(time.Millisecond * 50)
						pin1.DutyCycle(0, 12)
						time.Sleep(time.Millisecond * 50)
					default:
						fmt.Println("default switch on message")
					}
				})
			})

			// Set the handler for ICE connection state
			// This will notify you when the peer has connected/disconnected
			peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
				fmt.Printf("Connection State has changed %s \n", connectionState.String())
			})

			peerConnection.OnICECandidate(func(i *webrtc.ICECandidate) {
				if i == nil {
					return
				}

				candidateString, err := json.Marshal(i.ToJSON())
				if err != nil {
					log.Println(err)
					return
				}

				if writeErr := c.WriteJSON(&BaseMessage{
					Topic: "candidate",
					Data:  string(candidateString),
				}); writeErr != nil {
					log.Println(writeErr)
				}
			})

			s, _ := mediadevices.GetUserMedia(mediadevices.MediaStreamConstraints{
				Video: func(c *mediadevices.MediaTrackConstraints) {
					c.Height = prop.Int(144)
					c.Width = prop.Int(256)
				},
				Audio: func(c *mediadevices.MediaTrackConstraints) {
				},
				Codec: codecSelector,
			})

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

			answerSD, _ := json.Marshal(answer)

			if writeErr := c.WriteJSON(&BaseMessage{
				Topic: "answer",
				Data:  string(answerSD),
			}); writeErr != nil {
				log.Println(writeErr)
			}

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

}

func main() {
	if err := rpio.Open(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	defer rpio.Close()

	pin1.Mode(rpio.Pwm)
	pin1.Freq(50 * 12)
	pin1.DutyCycle(2, 12)
	time.Sleep(time.Millisecond * 100)
	pin1.DutyCycle(0, 12)

	HTTPServer()
}
