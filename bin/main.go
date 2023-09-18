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
	"sync"
	"os/exec"

	"github.com/gorilla/websocket"
	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/codec/opus"
	"github.com/pion/mediadevices/pkg/codec/vpx"
	"github.com/pion/mediadevices/pkg/prop"
	"github.com/pion/webrtc/v3"

	_ "github.com/pion/mediadevices/pkg/driver/audiotest"
	_ "github.com/pion/mediadevices/pkg/driver/camera"
)

var (
	peerConnection  *webrtc.PeerConnection
	peerConnections []peerConnectionState
	resolution = "480p"
	servoDeg = 0
)

type resolutioDimention struct {
	width uint16
	height uint16
}

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
	resDimention := &resolutioDimention{width: 854, height: 480}
	
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
		case "resolution":
			fmt.Println(msgRes.Data)
			resolution = msgRes.Data
			// Resolution with 16:9 aspect ratio
			switch(resolution) {
			case "240p":
				resDimention.width = 426
				resDimention.height = 240
			case "480p":
				return
			case "720p":
				resDimention.width = 1280
				resDimention.height = 720
			case "1080p":
				resDimention.width = 1920
				resDimention.height = 1080
			default:
				resDimention.width = 854
				resDimention.height = 480
			}
		case "offer":
			fmt.Print(resDimention.width)
			fmt.Printf("resolution width: %d, height: %d", resDimention.width, resDimention.height)
		
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
			vp8Params.BitRate = 1000_000 // 1MB

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
					c.Height = prop.Int(resDimention.height)
					c.Width = prop.Int(resDimention.width)
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
			
			// Register data channel creation handling
			peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
				fmt.Printf("New DataChannel %s %d\n", d.Label(), d.ID())

				// Register channel opening handling
				d.OnOpen(func() {
					fmt.Printf("Data channel '%s'-'%d' open\n", d.Label(), d.ID())
				})

				// Register text message handling
				d.OnMessage(func(msg webrtc.DataChannelMessage) {
					switch string(msg.Data) {
					case "go-right":
						if servoDeg >= 180 {
							return
						}
						servoDeg += 10
						fmt.Printf("go-right: %d", servoDeg)
						exec.Command("python3", "main.py", strconv.Itoa(servoDeg)).Run()
					case "go-left":
						if servoDeg <= 0 {
							return
						}
						servoDeg -= 10
						fmt.Printf("go-left: %d", servoDeg)
						exec.Command("python3", "main.py", strconv.Itoa(servoDeg)).Run()
					default:
						servoDeg = 0
					}
				})
			})

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
	HTTPServer()
}
