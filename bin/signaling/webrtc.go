package signaling

import (
	"encoding/json"
	"log"

	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/codec/mmal"

	// "github.com/pion/mediadevices/pkg/codec/opus"

	_ "github.com/pion/mediadevices/pkg/driver/camera"

	// _ "github.com/pion/mediadevices/pkg/driver/microphone"
	"github.com/pion/mediadevices/pkg/frame"
	"github.com/pion/mediadevices/pkg/prop"
	"github.com/pion/webrtc/v3"
)

var peerConnection *webrtc.PeerConnection

func addIceCandidate(candidate webrtc.ICECandidateInit) {
	peerConnection.AddICECandidate(candidate)
}

func NewWRTC() {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	offer := webrtc.SessionDescription{}
	json.Unmarshal(<-SessionDescriptionOffer, &offer)

	log.Printf("after %v: ", offer)

	// create new RTCPeerConnection
	mmalParams, err := mmal.NewParams()
	if err != nil {
		panic(err)
	}
	mmalParams.BitRate = 500_000 // 500kbps

	// opusParams, err := opus.NewParams()
	// if err != nil {
	// 	panic(err)
	// }

	codecSelector := mediadevices.NewCodecSelector(
		mediadevices.WithVideoEncoders(&mmalParams),
		// mediadevices.WithAudioEncoders(&opusParams),
	)

	mediaEngine := webrtc.MediaEngine{}
	codecSelector.Populate(&mediaEngine)
	api := webrtc.NewAPI(webrtc.WithMediaEngine(&mediaEngine))
	peerConnection, err = api.NewPeerConnection(config)
	if err != nil {
		panic(err)
	}

	// ice connection state
	peerConnection.OnICEConnectionStateChange(func(connectionState webrtc.ICEConnectionState) {
		log.Printf("connection state has change %s \n", connectionState.String())
	})

	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		iceCandidate <- candidate
	})

	s, err := mediadevices.GetUserMedia(mediadevices.MediaStreamConstraints{
		Video: func(c *mediadevices.MediaTrackConstraints) {
			c.FrameFormat = prop.FrameFormat(frame.FormatI420)
			c.Width = prop.Int(640)
			c.Height = prop.Int(480)
		},
		// Audio: func(c *mediadevices.MediaTrackConstraints) {
		// },
		// Codec: codecSelector,
	})
	if err != nil {
		panic(err)
	}

	for _, track := range s.GetTracks() {
		track.OnEnded(func(err error) {
			log.Printf("Track (ID: %s) ended with error: %v\n", track.ID(), err)
		})

		_, err = peerConnection.AddTransceiverFromTrack(track,
			webrtc.RTPTransceiverInit{
				Direction: webrtc.RTPTransceiverDirectionSendonly,
			},
		)
		if err != nil {
			panic(err)
		}
	}

	// set the remote sessionDescription
	err = peerConnection.SetRemoteDescription(offer)
	if err != nil {
		panic(err)
	}

	// create an answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		panic(err)
	}
	log.Printf("answer: %v", answer)
	log.Println("sending sessionDescription answer channel")
	SessionDescriptionAnswer <- answer

	// sets the localDescription and starts our UDP listener
	err = peerConnection.SetLocalDescription(answer)
	if err != nil {
		panic(err)
	}
	log.Println("setlocaldescription")

}
