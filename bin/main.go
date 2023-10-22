package main

import (
	"encoding/json"
	"flag"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"sync"
	"os/exec"
	"fmt"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/codec/opus"
	"github.com/pion/mediadevices/pkg/codec/vpx"
	"github.com/pion/mediadevices/pkg/prop"
	"github.com/pion/webrtc/v3"

//	_ "github.com/pion/mediadevices/pkg/driver/microphone"
//	_ "github.com/pion/mediadevices/pkg/driver/camera"
	_ "github.com/pion/mediadevices/pkg/driver/videotest"
	_ "github.com/pion/mediadevices/pkg/driver/audiotest"
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
			}else if websocket.IsCloseError(err, 1001) {
				log.Printf("connection close: %v", err)
			}
			break
		}

		msgRes := BaseMessage{}

		switch json.Unmarshal(msg, &msgRes); msgRes.Topic {
		case "resolution":
			log.Println(msgRes.Data)
			resolution = msgRes.Data
			// Resolution with 16:9 aspect ratio
			switch(resolution) {
			case "240p":
				resDimention.width = 426
				resDimention.height = 240
			case "480p":
				resDimention.width = 854
				resDimention.height = 480
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
			log.Printf("resolution width: %d, height: %d", resDimention.width, resDimention.height)
		
			config := webrtc.Configuration{
				ICEServers: []webrtc.ICEServer{
					{
						URLs: []string{"stun:stun.l.google.com:19302"},
					},
				},
			}

			// Create a new RTCPeerConnection
			// Register video codec
			vp8Params, err := vpx.NewVP8Params()
			if err != nil {
				panic(err)
			}
			log.Println(vp8Params.RTPCodec())
			vp8Params.BitRate = 1_000_000 // 1MB

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
			defer func() {
				if cErr := peerConnection.Close(); cErr != nil {
					log.Printf("cannot close peerConnection %v\n", cErr)
				}
			}()

			// Set the handler for ICE connection state
			// This will notify you when the peer has connected/disconnected
			peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
				log.Printf("Connection State has changed %s \n", connectionState.String())
				if connectionState.String() == "closed" || connectionState.String() == "disconnected" || connectionState.String() == "failed"{
					peerConnection.Close()
				}
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
				log.Printf("candidate browser %v \n", candidateString)

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
			
			if (s != nil) {
				for _, track := range s.GetTracks() {
				defer track.Close()
				log.Println(track.Kind())
				track.OnEnded(func(err error) {
					log.Printf("Track (ID: %s) ended with error: %v\n",
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
			}
			
			// Register data channel creation handling
			peerConnection.OnDataChannel(func(d *webrtc.DataChannel) {
				log.Printf("New DataChannel %s %d\n", d.Label(), d.ID())

				// Register channel opening handling
				d.OnOpen(func() {
					log.Printf("Data channel '%s'-'%d' open\n", d.Label(), d.ID())
				})

				// Register text message handling
				d.OnMessage(func(msg webrtc.DataChannelMessage) {
					// timing
					timeNow := time.Now().String()
					switch string(msg.Data) {
					case "go-right":
						timeNow = time.Now().String()
						servoDeg += 10
						exec.Command("python3", "main.py", strconv.Itoa(servoDeg)).Run()
						fmt.Println(timeNow[17:29])
					case "go-left":
						timeNow = time.Now().String()
						servoDeg -= 10
						exec.Command("python3", "main.py", strconv.Itoa(servoDeg)).Run()
						fmt.Println(fmt.Println(timeNow[17:29]))
					default:
						servoDeg = 0
					}
				})
			})

			// Set the remote SessionDescription
			offer := webrtc.SessionDescription{}
			err = json.Unmarshal([]byte(msgRes.Data), &offer)
			if err != nil {
				log.Println(err)
			}
			log.Println(offer)
			err = peerConnection.SetRemoteDescription(offer)
			if err != nil {
				panic(err)
			}

			// Create an answer
			answer, err := peerConnection.CreateAnswer(nil)
			if err != nil {
				panic(err)
			}
			log.Println("ctx: create answer")
			// Sets the LocalDescription, and starts our UDP listeners
			err = peerConnection.SetLocalDescription(answer)
			if err != nil {
				panic(err)
			}

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
			log.Printf("candidate : &v", candidate)
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
